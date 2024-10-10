package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/google/uuid"
	"github.com/hashicorp/go-plugin"
	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	openaiplugin "github.com/opencost/opencost-plugins/pkg/plugins/openai/openaiplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
)

// handshakeConfigs are used to just do a basic handshake between
// a plugin and host. If the handshake fails, a user friendly error is shown.
// This prevents users from executing bad plugins or executing a plugin
// directory. It is a UX feature, not a security feature.
var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "openai",
}

const openAIUsageURLFmt = "https://api.openai.com/v1/usage?date=%s"
const openAIBillingURLFmt = "https://api.openai.com/v1/dashboard/billing/usage/export?exclude_project_costs=false&file_format=json&new_endpoint=true&project_id&start_date=%s&end_date=%s"
const openAIAPIDateFormat = "2006-01-02"

// Implementation of CustomCostSource
type OpenAICostSource struct {
	rateLimiter *rate.Limiter
	config      *openaiplugin.OpenAIConfig
}

func (d *OpenAICostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
	results := []*pb.CustomCostResponse{}

	targets, err := opencost.GetWindows(req.Start.AsTime(), req.End.AsTime(), req.Resolution.AsDuration())
	if err != nil {
		log.Errorf("error getting windows: %v", err)
		errResp := pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting windows: %v", err)},
		}
		results = append(results, &errResp)
		return results
	}

	if req.Resolution.AsDuration() != timeutil.Day {
		log.Infof("openai plugin only supports daily resolution")
		return results
	}

	for _, target := range targets {
		// don't allow future request
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		log.Debugf("fetching Open AI costs for window %v", target)
		result := d.getOpenAICostsForWindow(target)
		results = append(results, result)
	}

	return results
}

func main() {

	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	oaiConfig, err := getOpenAIConfig(configFile)
	if err != nil {
		log.Fatalf("error building OpenAI config: %v", err)
	}
	log.SetLogLevel(oaiConfig.LogLevel)
	// rate limit to 1 request per second
	rateLimiter := rate.NewLimiter(0.5, 1)
	oaiCostSrc := OpenAICostSource{
		rateLimiter: rateLimiter,
		config:      oaiConfig,
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &oaiCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

func boilerplateOpenAICustomCost(win opencost.Window) pb.CustomCostResponse {
	return pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v1"},
		CostSource: "AI",
		Domain:     "openai",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      []*pb.CustomCost{},
	}
}
func (d *OpenAICostSource) getOpenAICostsForWindow(window opencost.Window) *pb.CustomCostResponse {
	ccResp := boilerplateOpenAICustomCost(window)

	oaiTokenUsages, err := d.getOpenAITokenUsages(*window.Start())
	if err != nil {
		ccResp.Errors = append(ccResp.Errors, fmt.Sprintf("error getting OpenAI token usages: %v", err))
	}

	oaiBilling, err := d.getOpenAIBilling(*window.Start(), *window.End())
	if err != nil {
		ccResp.Errors = append(ccResp.Errors, fmt.Sprintf("error getting OpenAI billing data: %v", err))
	}

	customCosts, err := getCustomCostsFromUsageAndBilling(oaiTokenUsages, oaiBilling)
	if err != nil {
		ccResp.Errors = append(ccResp.Errors, fmt.Sprintf("error converting API responses into custom costs: %v", err))
	}
	ccResp.Costs = customCosts

	return &ccResp
}

func getCustomCostsFromUsageAndBilling(usage *openaiplugin.OpenAIUsage, billing *openaiplugin.OpenAIBilling) ([]*pb.CustomCost, error) {
	customCosts := []*pb.CustomCost{}

	tokenMap := buildTokenMap(usage)
	for _, billingEntry := range billing.Data {
		tokenMapKey := strings.ReplaceAll(strings.ToLower(billingEntry.Name), "-", "")
		tokenMapKey = strings.ReplaceAll(tokenMapKey, " ", "")
		tokenMapKey = strings.ReplaceAll(tokenMapKey, "_", "")

		tokenCount, ok := tokenMap[tokenMapKey]
		if !ok {
			log.Debugf("no token usage found for %s", billingEntry.Name)
			tokenCount = -1
		}

		extendedAttrs := pb.CustomCostExtendedAttributes{
			AccountId:    &billingEntry.OrganizationID,
			SubAccountId: &billingEntry.ProjectID,
		}
		customCost := pb.CustomCost{
			BilledCost:         float32(billingEntry.CostInMajor),
			AccountName:        billingEntry.OrganizationName,
			ChargeCategory:     "Usage",
			Description:        fmt.Sprintf("OpenAI usage for model %s", billingEntry.Name),
			ResourceName:       billingEntry.Name,
			ResourceType:       "AI Model",
			Id:                 uuid.New().String(),
			ProviderId:         fmt.Sprintf("%s/%s/%s", billingEntry.OrganizationID, billingEntry.ProjectID, billingEntry.Name),
			UsageQuantity:      float32(tokenCount),
			UsageUnit:          "tokens - All snapshots, all projects",
			ExtendedAttributes: &extendedAttrs,
		}

		customCosts = append(customCosts, &customCost)
	}

	return customCosts, nil
}

var snapshotRe = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}|-`)

func buildTokenMap(usage *openaiplugin.OpenAIUsage) map[string]int {
	tokenMap := make(map[string]int)
	if usage == nil {
		return tokenMap
	}
	for _, usageData := range usage.Data {
		key := snapshotRe.ReplaceAllString(usageData.SnapshotID, "")
		key = strings.ToLower(key)
		if _, ok := tokenMap[key]; !ok {
			tokenMap[key] = 0
		}

		tokenMap[key] += (usageData.NGeneratedTokensTotal + usageData.NContextTokensTotal)
	}
	return tokenMap
}

func (d *OpenAICostSource) getOpenAIBilling(start time.Time, end time.Time) (*openaiplugin.OpenAIBilling, error) {
	client := &http.Client{}
	openAIBillingURL := fmt.Sprintf(openAIBillingURLFmt, start.Format(openAIAPIDateFormat), end.Format(openAIAPIDateFormat))
	log.Debugf("fetching OpenAI billing data from %s", openAIBillingURL)
	var errReq error
	var resp *http.Response
	for i := 0; i < 3; i++ {
		err := d.rateLimiter.Wait(context.Background())
		if err != nil {
			log.Warnf("error waiting for rate limiter: %v", err)
			return nil, fmt.Errorf("error waiting for rate limiter: %v", err)
		}
		var req *http.Request
		req, errReq = http.NewRequest("GET", openAIBillingURL, nil)
		if errReq != nil {
			log.Warnf("error creating billing export request: %v", errReq)
			log.Warnf("retrying request after 30s")
			time.Sleep(30 * time.Second)
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", d.config.APIKey))

		resp, errReq = client.Do(req)
		if errReq != nil {
			log.Warnf("error doing billing export request: %v", errReq)
			log.Warnf("retrying requestafter 30s")
			time.Sleep(30 * time.Second)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			bodyBytes, err := io.ReadAll(resp.Body)
			bodyString := "<empty>"
			if err != nil {
				log.Warnf("error reading body of non-200 response: %v", err)
			} else {
				bodyString = string(bodyBytes)
			}

			errReq = fmt.Errorf("received non-200 response for billing export request: %d", resp.StatusCode)
			log.Warnf("got non-200 response for billing export request: %d, body is: %s", resp.StatusCode, bodyString)
			log.Warnf("retrying request after 30s")
			time.Sleep(30 * time.Second)
			continue
		} else {
			errReq = nil
		}
		// request was successful, break out of loop
		break
	}

	if errReq != nil {
		return nil, fmt.Errorf("error making request after retries: %v", errReq)
	}
	var billingData openaiplugin.OpenAIBilling
	if err := json.NewDecoder(resp.Body).Decode(&billingData); err != nil {
		return nil, fmt.Errorf("error decoding billing export response: %v", err)
	}
	resp.Body.Close()
	for i := range billingData.Data {
		asFloat, err := strconv.ParseFloat(billingData.Data[i].CostInMajorStr, 64)
		if err != nil {
			return nil, fmt.Errorf("error parsing cost: %v", err)
		}
		billingData.Data[i].CostInMajor = asFloat
	}

	return &billingData, nil
}

func (d *OpenAICostSource) getOpenAITokenUsages(targetTime time.Time) (*openaiplugin.OpenAIUsage, error) {
	client := &http.Client{}

	openAIUsageURL := fmt.Sprintf(openAIUsageURLFmt, targetTime.Format(openAIAPIDateFormat))
	log.Debugf("fetching OpenAI usage data from %s", openAIUsageURL)
	var errReq error
	var resp *http.Response
	for i := 0; i < 3; i++ {
		errReq = nil
		err := d.rateLimiter.Wait(context.Background())
		if err != nil {
			log.Warnf("error waiting for rate limiter: %v", err)
			return nil, fmt.Errorf("error waiting for rate limiter: %v", err)
		}
		var req *http.Request
		req, errReq = http.NewRequest("GET", openAIUsageURL, nil)
		if errReq != nil {
			log.Warnf("error creating usage request: %v", errReq)
			log.Warnf("retrying request after 30s")
			time.Sleep(30 * time.Second)
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", d.config.APIKey))

		resp, errReq = client.Do(req)
		if errReq != nil {
			log.Warnf("error doing token request: %v", errReq)
			log.Warnf("retrying request after 30s")
			time.Sleep(30 * time.Second)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errReq = fmt.Errorf("received non-200 response for token usage request: %d", resp.StatusCode)
			bodyBytes, err := io.ReadAll(resp.Body)
			bodyString := "<empty>"
			if err != nil {
				log.Warnf("error reading body of non-200 response: %v", err)
			} else {
				bodyString = string(bodyBytes)
			}
			log.Warnf("got non-200 response for token usage request: %d, body is: %s", resp.StatusCode, bodyString)
			log.Warnf("retrying request after 30s")
			time.Sleep(30 * time.Second)
			continue
		} else {
			errReq = nil
		}
		// request was successful, break out of loop
		break
	}

	if errReq != nil {
		return nil, fmt.Errorf("error making request after retries: %v", errReq)
	}

	var usageData openaiplugin.OpenAIUsage
	if err := json.NewDecoder(resp.Body).Decode(&usageData); err != nil {
		return nil, fmt.Errorf("error decoding response: %v", err)
	}

	return &usageData, nil
}

func getOpenAIConfig(configFilePath string) (*openaiplugin.OpenAIConfig, error) {
	var result openaiplugin.OpenAIConfig
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file for openai config @ %s: %v", configFilePath, err)
	}
	err = json.Unmarshal(bytes, &result)
	if err != nil {
		return nil, fmt.Errorf("error marshaling json into openai config %v", err)
	}

	if result.LogLevel == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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

var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "openai",
}

const openAICostsURL = "https://api.openai.com/v1/organization/costs"
const openAICompletionsUsageURL = "https://api.openai.com/v1/organization/usage/completions"

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
	rateLimiter := rate.NewLimiter(0.5, 1)
	oaiCostSrc := OpenAICostSource{
		rateLimiter: rateLimiter,
		config:      oaiConfig,
	}

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

	costsResp, err := d.getOpenAICosts(*window.Start(), *window.End())
	if err != nil {
		ccResp.Errors = append(ccResp.Errors, fmt.Sprintf("error getting OpenAI costs: %v", err))
	}

	usageResp, err := d.getOpenAICompletionsUsage(*window.Start(), *window.End())
	if err != nil {
		ccResp.Errors = append(ccResp.Errors, fmt.Sprintf("error getting OpenAI usage: %v", err))
	}

	customCosts := buildCustomCosts(costsResp, usageResp)
	ccResp.Costs = customCosts

	return &ccResp
}

// buildCustomCosts merges cost data (dollar amounts per line_item/project) with
// usage data (token counts per model/project) into FOCUS-compliant CustomCost entries.
//
// The costs API groups by line_item (e.g. "GPT-4o", "Embeddings") and project_id.
// The usage API groups by model (e.g. "gpt-4o-2024-08-06") and project_id.
// We use the costs response as the primary source of truth for billed amounts, and
// attach token counts where we can match on a normalized model/line_item key.
func buildCustomCosts(costs *openaiplugin.CostsResponse, usage *openaiplugin.CompletionsUsageResponse) []*pb.CustomCost {
	tokenMap := buildTokenMap(usage)
	var customCosts []*pb.CustomCost

	if costs == nil {
		return customCosts
	}

	for _, bucket := range costs.Data {
		for _, result := range bucket.Results {
			lineItem := derefStr(result.LineItem, "Unknown")
			projectID := derefStr(result.ProjectID, "")
			cost := result.Amount.Value

			if cost == 0 {
				log.Debugf("skipping zero-cost line item %s", lineItem)
				continue
			}

			tokenKey := normalizeModelKey(lineItem) + "|" + projectID
			tokenCount, hasTokens := tokenMap[tokenKey]
			if !hasTokens {
				tokenKeyNoProject := normalizeModelKey(lineItem) + "|"
				tokenCount, hasTokens = tokenMap[tokenKeyNoProject]
			}

			usageQty := float32(-1)
			if hasTokens {
				usageQty = float32(tokenCount)
			}

			extendedAttrs := pb.CustomCostExtendedAttributes{
				SubAccountId: strPtr(projectID),
			}
			cc := pb.CustomCost{
				BilledCost:         float32(cost),
				ChargeCategory:     "Usage",
				Description:        fmt.Sprintf("OpenAI usage for %s", lineItem),
				ResourceName:       lineItem,
				ResourceType:       "AI Model",
				Id:                 uuid.New().String(),
				ProviderId:         fmt.Sprintf("%s/%s", projectID, lineItem),
				UsageQuantity:      usageQty,
				UsageUnit:          "tokens",
				ExtendedAttributes: &extendedAttrs,
			}

			customCosts = append(customCosts, &cc)
		}
	}

	return customCosts
}

// buildTokenMap aggregates token counts from the completions usage response,
// keyed by normalizedModel|projectID.
func buildTokenMap(usage *openaiplugin.CompletionsUsageResponse) map[string]int {
	tokenMap := make(map[string]int)
	if usage == nil {
		return tokenMap
	}
	for _, bucket := range usage.Data {
		for _, result := range bucket.Results {
			model := derefStr(result.Model, "unknown")
			projectID := derefStr(result.ProjectID, "")
			key := normalizeModelKey(model) + "|" + projectID
			tokenMap[key] += result.InputTokens + result.OutputTokens
		}
	}
	return tokenMap
}

// normalizeModelKey strips date suffixes, dashes, spaces, underscores, and lowercases
// so that "GPT-4o mini" and "gpt-4o-mini-2024-07-18" both map to "gpt4omini".
func normalizeModelKey(s string) string {
	s = strings.ToLower(s)
	// strip trailing date suffix like "-2024-07-18" (11 chars: dash + YYYY-MM-DD)
	if len(s) > 11 {
		candidate := s[len(s)-10:]
		if s[len(s)-11] == '-' && isDateLike(candidate) {
			s = s[:len(s)-11]
		}
	}
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func isDateLike(s string) bool {
	if len(s) != 10 {
		return false
	}
	for i, c := range s {
		if i == 4 || i == 7 {
			if c != '-' {
				return false
			}
		} else if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func derefStr(p *string, fallback string) string {
	if p != nil {
		return *p
	}
	return fallback
}

func strPtr(s string) *string {
	return &s
}

// getOpenAICosts fetches cost data from /v1/organization/costs grouped by
// line_item and project_id. Handles pagination.
func (d *OpenAICostSource) getOpenAICosts(start, end time.Time) (*openaiplugin.CostsResponse, error) {
	params := url.Values{}
	params.Set("start_time", strconv.FormatInt(start.Unix(), 10))
	params.Set("end_time", strconv.FormatInt(end.Unix(), 10))
	params.Set("bucket_width", "1d")
	params.Set("limit", "31")
	params.Add("group_by", "line_item")
	params.Add("group_by", "project_id")

	var allBuckets []openaiplugin.CostsTimeBucket
	page := ""

	for {
		if page != "" {
			params.Set("page", page)
		}

		fullURL := openAICostsURL + "?" + params.Encode()
		log.Debugf("fetching OpenAI costs from %s", fullURL)

		body, err := d.doRequestWithRetry("GET", fullURL)
		if err != nil {
			return nil, fmt.Errorf("costs request failed: %w", err)
		}

		var resp openaiplugin.CostsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("error decoding costs response: %w", err)
		}

		allBuckets = append(allBuckets, resp.Data...)

		if !resp.HasMore || resp.NextPage == "" {
			break
		}
		page = resp.NextPage
	}

	return &openaiplugin.CostsResponse{Data: allBuckets}, nil
}

// getOpenAICompletionsUsage fetches token usage from /v1/organization/usage/completions
// grouped by model and project_id. Handles pagination.
func (d *OpenAICostSource) getOpenAICompletionsUsage(start, end time.Time) (*openaiplugin.CompletionsUsageResponse, error) {
	params := url.Values{}
	params.Set("start_time", strconv.FormatInt(start.Unix(), 10))
	params.Set("end_time", strconv.FormatInt(end.Unix(), 10))
	params.Set("bucket_width", "1d")
	params.Set("limit", "31")
	params.Add("group_by", "model")
	params.Add("group_by", "project_id")

	var allBuckets []openaiplugin.CompletionsTimeBucket
	page := ""

	for {
		if page != "" {
			params.Set("page", page)
		}

		fullURL := openAICompletionsUsageURL + "?" + params.Encode()
		log.Debugf("fetching OpenAI completions usage from %s", fullURL)

		body, err := d.doRequestWithRetry("GET", fullURL)
		if err != nil {
			return nil, fmt.Errorf("completions usage request failed: %w", err)
		}

		var resp openaiplugin.CompletionsUsageResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("error decoding completions usage response: %w", err)
		}

		allBuckets = append(allBuckets, resp.Data...)

		if !resp.HasMore || resp.NextPage == "" {
			break
		}
		page = resp.NextPage
	}

	return &openaiplugin.CompletionsUsageResponse{Data: allBuckets}, nil
}

// doRequestWithRetry performs an authenticated GET with up to 3 retries on failure.
func (d *OpenAICostSource) doRequestWithRetry(method, fullURL string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := d.rateLimiter.Wait(context.Background()); err != nil {
			return nil, fmt.Errorf("rate limiter error: %w", err)
		}

		req, err := http.NewRequest(method, fullURL, nil)
		if err != nil {
			lastErr = fmt.Errorf("error creating request: %w", err)
			log.Warnf("%v, retrying in 30s", lastErr)
			time.Sleep(30 * time.Second)
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", d.config.APIKey))

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			log.Warnf("%v, retrying in 30s", lastErr)
			time.Sleep(30 * time.Second)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("error reading response body: %w", err)
			log.Warnf("%v, retrying in 30s", lastErr)
			time.Sleep(30 * time.Second)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body[:min(len(body), 500)]))
			log.Warnf("non-200 response: %v, retrying in 30s", lastErr)
			time.Sleep(30 * time.Second)
			continue
		}

		return body, nil
	}

	return nil, fmt.Errorf("request failed after 3 attempts: %w", lastErr)
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

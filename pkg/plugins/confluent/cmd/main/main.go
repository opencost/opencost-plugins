package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-plugin"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	confluentconfig "github.com/opencost/opencost-plugins/pkg/plugins/confluent/config"
	confluentplugin "github.com/opencost/opencost-plugins/pkg/plugins/confluent/confluentplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
)

// handshakeConfigs are used to just do a basic handshake between
// a plugin and host. If the handshake fails, a user friendly error is shown.
var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "confluent",
}

const confluentCostsURL = "https://api.confluent.cloud/billing/v1/costs"
const confluentAPIDateFormat = "2006-01-02"

// Implementation of CustomCostSource
type ConfluentCostSource struct {
	rateLimiter *rate.Limiter
	config      *confluentconfig.ConfluentConfig
	client      HTTPClient
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

func (c *ConfluentCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
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

	for _, target := range targets {
		// don't allow future requests
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		log.Debugf("fetching Confluent costs for window %v", target)
		result := c.getConfluentCostsForWindow(target)
		results = append(results, result)
	}

	return results
}

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	confluentCfg, err := confluentconfig.GetConfluentConfig(configFile)
	if err != nil {
		log.Fatalf("error building Confluent config: %v", err)
	}
	log.SetLogLevel(confluentCfg.LogLevel)

	// Confluent Cloud API rate limit: approximately 100 requests per minute
	rateLimiter := rate.NewLimiter(1.5, 2)
	confluentCostSrc := ConfluentCostSource{
		rateLimiter: rateLimiter,
		config:      confluentCfg,
		client:      &http.Client{},
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &confluentCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

func boilerplateConfluentCustomCost(win opencost.Window) pb.CustomCostResponse {
	return pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v1"},
		CostSource: "data_streaming",
		Domain:     "confluent",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      []*pb.CustomCost{},
	}
}

func (c *ConfluentCostSource) getConfluentCostsForWindow(window opencost.Window) *pb.CustomCostResponse {
	ccResp := boilerplateConfluentCustomCost(window)

	winStart := window.Start().Format(confluentAPIDateFormat)
	winEnd := window.End().Format(confluentAPIDateFormat)

	// Fetch costs for the window period
	costs, err := c.getCosts(winStart, winEnd)
	if err != nil {
		ccResp.Errors = append(ccResp.Errors, fmt.Sprintf("error getting Confluent costs: %v", err))
		return &ccResp
	}

	customCosts := []*pb.CustomCost{}
	for _, cost := range costs {
		resourceDisplayName := cost.Resource.DisplayName
		resourceID := cost.Resource.ID
		environmentID := cost.Resource.Environment.ID

		customCost := pb.CustomCost{
			AccountName:    environmentID,
			ChargeCategory: "Usage",
			Description:    fmt.Sprintf("Confluent %s usage", cost.Product),
			ResourceName:   cost.Product,
			ResourceType:   cost.LineType,
			Id:             uuid.New().String(),
			ProviderId:     fmt.Sprintf("%s/%s/%s", resourceID, cost.Product, cost.LineType),
			BilledCost:     float32(cost.Amount),
			ListCost:       float32(cost.OriginalAmount),
			ListUnitPrice:  float32(cost.Price),
			UsageQuantity:  float32(cost.Quantity),
			UsageUnit:      cost.Unit,
			Labels:         map[string]string{},
		}

		if resourceDisplayName != "" {
			customCost.Labels["resource_display_name"] = resourceDisplayName
		}
		if environmentID != "" {
			customCost.Labels["environment_id"] = environmentID
		}
		if cost.NetworkAccessType != "" {
			customCost.Labels["network_access_type"] = cost.NetworkAccessType
		}
		if cost.Granularity != "" {
			customCost.Labels["granularity"] = cost.Granularity
		}
		if cost.DiscountAmount > 0 {
			customCost.Labels["discount_amount"] = fmt.Sprintf("%.6f", cost.DiscountAmount)
		}
		if cost.ID != "" {
			customCost.Labels["cost_id"] = cost.ID
		}

		customCosts = append(customCosts, &customCost)
	}

	ccResp.Costs = customCosts
	return &ccResp
}

func (c *ConfluentCostSource) getCosts(startDate, endDate string) ([]confluentplugin.ConfluentCost, error) {
	var allCosts []confluentplugin.ConfluentCost

	// Build Basic Auth header from API key and secret
	credentials := base64.StdEncoding.EncodeToString(
		[]byte(fmt.Sprintf("%s:%s", c.config.APIKey, c.config.APISecret)),
	)

	url := fmt.Sprintf("%s?start_date=%s&end_date=%s&page_size=100", confluentCostsURL, startDate, endDate)

	for {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("error creating request: %v", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("Basic %s", credentials))
		req.Header.Set("Accept", "application/json")

		err = c.rateLimiter.Wait(context.Background())
		if err != nil {
			return nil, fmt.Errorf("rate limiter error: %v", err)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error making request to Confluent API: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("Confluent API returned status %d: %s", resp.StatusCode, string(body))
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("error reading response body: %v", err)
		}

		var costList confluentplugin.ConfluentCostList
		err = json.Unmarshal(body, &costList)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling Confluent response: %v", err)
		}

		allCosts = append(allCosts, costList.Data...)

		// Check for pagination
		if costList.Metadata.NextPageToken != "" {
			url = fmt.Sprintf("%s?start_date=%s&end_date=%s&page_size=100&page_token=%s",
				confluentCostsURL, startDate, endDate, costList.Metadata.NextPageToken)
		} else {
			break
		}
	}

	return allCosts, nil
}

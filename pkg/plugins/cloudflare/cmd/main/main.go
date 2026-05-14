package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/hashicorp/go-plugin"
	commonconfig "github.com/opencost/opencost-plugins/common/config"
	cloudflareplugin "github.com/opencost/opencost-plugins/pkg/plugins/cloudflare/cloudflareplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/util/uuid"
)

// handshakeConfigs are used to just do a basic handshake between
// a plugin and host. If the handshake fails, a user friendly error is shown.
var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "cloudflare",
}

const (
	cloudflareBaseURL = "https://api.cloudflare.com/client/v4"
	zonesEndpoint     = "/zones"
	billingEndpoint   = "/user/billing/history"
	subscriptionsEndpoint = "/subscriptions"
)

func main() {
	log.Debug("Initializing Cloudflare plugin")

	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	cfConfig, err := cloudflareplugin.GetCloudflareConfig(configFile)
	if err != nil {
		log.Fatalf("error building Cloudflare config: %v", err)
	}
	log.SetLogLevel(cfConfig.LogLevel)

	// Cloudflare API rate limit: 1200 requests per 5 minutes for Enterprise,
	// 1200 per 5 min for Business, 1200 per 5 min for Pro. Be conservative.
	rateLimiter := rate.NewLimiter(2.0, 5)
	cfCostSrc := CloudflareCostSource{
		rateLimiter: rateLimiter,
		apiKey:      cfConfig.APIKey,
		email:       cfConfig.Email,
		accountID:   cfConfig.AccountID,
		httpClient:  &http.Client{},
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &cfCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

// HTTPClient interface for testability
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// CloudflareCostSource implements CustomCostSource
type CloudflareCostSource struct {
	apiKey      string
	email       string
	accountID   string
	rateLimiter *rate.Limiter
	httpClient  HTTPClient
}

func (c *CloudflareCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
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

	// Fetch billing history
	billingHistory, err := c.getBillingHistory()
	if err != nil {
		log.Errorf("error fetching Cloudflare billing history: %v", err)
		errResp := pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error fetching Cloudflare billing history: %v", err)},
		}
		results = append(results, &errResp)
		return results
	}

	// Fetch zones for enrichment
	zones, err := c.getZones()
	if err != nil {
		log.Warnf("error fetching Cloudflare zones, proceeding without enrichment: %v", err)
		zones = []cloudflareplugin.CloudflareZone{}
	}

	zoneMap := makeZoneMap(zones)

	// Fetch subscriptions
	subscriptions, err := c.getSubscriptions()
	if err != nil {
		log.Warnf("error fetching Cloudflare subscriptions: %v", err)
		subscriptions = []CloudflareSubscription{}
	}

	for _, target := range targets {
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		log.Debugf("fetching Cloudflare costs for window %v", target)
		result := c.getCloudflareCostsForWindow(&target, billingHistory, zoneMap, subscriptions)
		results = append(results, result)
	}

	return results
}

// CloudflareSubscription represents a subscription
type CloudflareSubscription struct {
	ID         string  `json:"id"`
	State      string  `json:"state"`
	Price      float32 `json:"price"`
	Frequency  string  `json:"frequency"`
	Product    CloudflareProduct `json:"product"`
	Zone       CloudflareSubZone `json:"zone"`
}

// CloudflareProduct represents a product in a subscription
type CloudflareProduct struct {
	Name string `json:"name"`
}

// CloudflareSubZone represents a zone in a subscription
type CloudflareSubZone struct {
	Name string `json:"name"`
}

// SubscriptionsResponse wraps the API response
type SubscriptionsResponse struct {
	Result     []CloudflareSubscription `json:"result"`
	Success    bool                     `json:"success"`
	Errors     []cloudflareplugin.CloudflareAPIError `json:"errors"`
	ResultInfo cloudflareplugin.CloudflareResultInfo `json:"result_info"`
}

func (c *CloudflareCostSource) getBillingHistory() ([]cloudflareplugin.CloudflareBillingHistory, error) {
	var allBilling []cloudflareplugin.CloudflareBillingHistory
	page := 1
	perPage := 50

	for {
		url := fmt.Sprintf("%s%s?page=%d&per_page=%d&order=occurred_at&direction=desc",
			cloudflareBaseURL, billingEndpoint, page, perPage)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("error creating request: %v", err)
		}
		c.setAuthHeaders(req)

		if err := c.rateLimiter.Wait(context.Background()); err != nil {
			return nil, fmt.Errorf("rate limiter error: %v", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error making request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading response body: %v", err)
		}

		var billingResp cloudflareplugin.CloudflareBillingResponse
		if err := json.Unmarshal(body, &billingResp); err != nil {
			return nil, fmt.Errorf("error unmarshalling billing history: %v", err)
		}

		if !billingResp.Success {
			return nil, fmt.Errorf("API returned error: %v", billingResp.Errors)
		}

		allBilling = append(allBilling, billingResp.Result...)

		if page >= billingResp.ResultInfo.TotalPages || billingResp.ResultInfo.TotalPages == 0 {
			break
		}
		page++
	}

	return allBilling, nil
}

func (c *CloudflareCostSource) getZones() ([]cloudflareplugin.CloudflareZone, error) {
	var allZones []cloudflareplugin.CloudflareZone
	page := 1
	perPage := 50

	for {
		url := fmt.Sprintf("%s%s?page=%d&per_page=%d&status=active",
			cloudflareBaseURL, zonesEndpoint, page, perPage)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("error creating request: %v", err)
		}
		c.setAuthHeaders(req)

		if err := c.rateLimiter.Wait(context.Background()); err != nil {
			return nil, fmt.Errorf("rate limiter error: %v", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error making request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading response body: %v", err)
		}

		var zonesResp cloudflareplugin.CloudflareZonesResponse
		if err := json.Unmarshal(body, &zonesResp); err != nil {
			return nil, fmt.Errorf("error unmarshalling zones: %v", err)
		}

		if !zonesResp.Success {
			return nil, fmt.Errorf("API returned error: %v", zonesResp.Errors)
		}

		allZones = append(allZones, zonesResp.Result...)

		if page >= zonesResp.ResultInfo.TotalPages || zonesResp.ResultInfo.TotalPages == 0 {
			break
		}
		page++
	}

	return allZones, nil
}

func (c *CloudflareCostSource) getSubscriptions() ([]CloudflareSubscription, error) {
	var allSubs []CloudflareSubscription
	page := 1
	perPage := 50

	for {
		url := fmt.Sprintf("%s%s?page=%d&per_page=%d",
			cloudflareBaseURL, subscriptionsEndpoint, page, perPage)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("error creating request: %v", err)
		}
		c.setAuthHeaders(req)

		if err := c.rateLimiter.Wait(context.Background()); err != nil {
			return nil, fmt.Errorf("rate limiter error: %v", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error making request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading response body: %v", err)
		}

		var subsResp SubscriptionsResponse
		if err := json.Unmarshal(body, &subsResp); err != nil {
			return nil, fmt.Errorf("error unmarshalling subscriptions: %v", err)
		}

		if !subsResp.Success {
			return nil, fmt.Errorf("API returned error: %v", subsResp.Errors)
		}

		allSubs = append(allSubs, subsResp.Result...)

		if page >= subsResp.ResultInfo.TotalPages || subsResp.ResultInfo.TotalPages == 0 {
			break
		}
		page++
	}

	return allSubs, nil
}

func (c *CloudflareCostSource) setAuthHeaders(req *http.Request) {
	if c.email != "" {
		// X-Auth-Email + X-Auth-Key auth
		req.Header.Set("X-Auth-Email", c.email)
		req.Header.Set("X-Auth-Key", c.apiKey)
	} else {
		// Bearer token auth
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")
}

func makeZoneMap(zones []cloudflareplugin.CloudflareZone) map[string]cloudflareplugin.CloudflareZone {
	m := make(map[string]cloudflareplugin.CloudflareZone)
	for _, z := range zones {
		m[z.ID] = z
	}
	return m
}

func (c *CloudflareCostSource) getCloudflareCostsForWindow(
	win *opencost.Window,
	billingHistory []cloudflareplugin.CloudflareBillingHistory,
	zoneMap map[string]cloudflareplugin.CloudflareZone,
	subscriptions []CloudflareSubscription,
) *pb.CustomCostResponse {
	costs := []*pb.CustomCost{}

	winStartUTC := win.Start().UTC()
	winEndUTC := win.End().UTC()

	// Process billing history
	for _, entry := range billingHistory {
		occurredAt, err := time.Parse(time.RFC3339, entry.OccurredAt)
		if err != nil {
			// Try alternative format
			occurredAt, err = time.Parse("2006-01-02T15:04:05Z", entry.OccurredAt)
			if err != nil {
				log.Debugf("could not parse date: %s, skipping entry", entry.OccurredAt)
				continue
			}
		}

		occurredAtUTC := occurredAt.UTC()

		// Check if the billing entry falls within the window
		if (occurredAtUTC.After(winStartUTC) || occurredAtUTC.Equal(winStartUTC)) &&
			(occurredAtUTC.Before(winEndUTC) || occurredAtUTC.Equal(winEndUTC)) {

			description := entry.Description
			if description == "" {
				description = entry.TypeInfo.Description
			}

			productName := entry.TypeInfo.Item
			if productName == "" {
				productName = entry.Type
			}

			quantity := entry.TypeInfo.Quantity
			if quantity == 0 {
				quantity = 1
			}

			unit := entry.TypeInfo.Unit
			if unit == "" {
				unit = "item"
			}

			cost := &pb.CustomCost{
				Zone:           "global",
				AccountName:    c.email,
				ChargeCategory: "Usage",
				Description:    description,
				ResourceName:   productName,
				ResourceType:   "cloud_service",
				Id:             string(uuid.NewUUID()),
				ProviderId:     fmt.Sprintf("cloudflare/%s", entry.ID),
				BilledCost:     entry.Amount,
				ListCost:       entry.Amount,
				ListUnitPrice:  entry.Amount / quantity,
				UsageQuantity:  quantity,
				UsageUnit:      unit,
				Labels: map[string]string{
					"action":    entry.Action,
					"type":      entry.Type,
					"currency":  entry.Currency,
				},
			}
			costs = append(costs, cost)
		}
	}

	// Process subscription costs
	for _, sub := range subscriptions {
		if sub.State != "paid" {
			continue
		}

		productName := sub.Product.Name
		zoneName := sub.Zone.Name

		// Determine usage quantity based on frequency
		quantity := float32(1)
		usageUnit := "month"
		if sub.Frequency == "weekly" {
			quantity = float32(1)
			usageUnit = "week"
		} else if sub.Frequency == "daily" {
			quantity = float32(1)
			usageUnit = "day"
		}

		providerID := fmt.Sprintf("cloudflare/subscription/%s", sub.ID)
		resourceName := productName
		if zoneName != "" {
			providerID = fmt.Sprintf("cloudflare/subscription/%s/%s", sub.ID, zoneName)
			resourceName = fmt.Sprintf("%s (%s)", productName, zoneName)
		}

		cost := &pb.CustomCost{
			Zone:           "global",
			AccountName:    zoneName,
			ChargeCategory: "Subscription",
			Description:    fmt.Sprintf("Cloudflare %s subscription", productName),
			ResourceName:   resourceName,
			ResourceType:   "subscription",
			Id:             string(uuid.NewUUID()),
			ProviderId:     providerID,
			BilledCost:     sub.Price,
			ListCost:       sub.Price,
			ListUnitPrice:  sub.Price / quantity,
			UsageQuantity:  quantity,
			UsageUnit:      usageUnit,
			Labels: map[string]string{
				"state":     sub.State,
				"frequency": sub.Frequency,
			},
		}
		costs = append(costs, cost)
	}

	// Enrich costs with zone information where possible
	for _, cost := range costs {
		for zoneID, zone := range zoneMap {
			if cost.AccountName == zone.Name || cost.Labels["zone_id"] == zoneID {
				cost.Labels["zone_id"] = zone.ID
				cost.Labels["zone_status"] = zone.Status
				cost.Labels["zone_plan"] = zone.Plan.Name
				break
			}
		}
	}

	currency := "USD"
	if len(billingHistory) > 0 && billingHistory[0].Currency != "" {
		currency = billingHistory[0].Currency
	}

	resp := pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v4"},
		CostSource: "cdn",
		Domain:     "cloudflare",
		Version:    "v1",
		Currency:   currency,
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      costs,
	}
	return &resp
}

// parseIntOrDefault parses a string as int, returning default on failure
func parseIntOrDefault(s string, defaultVal int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-plugin"
	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	confluentplugin "github.com/opencost/opencost-plugins/pkg/plugins/confluent/confluentplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	confluentDateFormat = "2006-01-02"
)

var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "confluent",
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type ConfluentCostSource struct {
	client HTTPClient
	config *confluentplugin.ConfluentConfig
}

type costList struct {
	APIVersion string           `json:"api_version"`
	Kind       string           `json:"kind"`
	Metadata   costListMetadata `json:"metadata"`
	Data       []costItem       `json:"data"`
}

type costListMetadata struct {
	Next string `json:"next"`
}

type costItem struct {
	ID                string            `json:"id"`
	StartDate         string            `json:"start_date"`
	EndDate           string            `json:"end_date"`
	Granularity       string            `json:"granularity"`
	NetworkAccessType string            `json:"network_access_type"`
	Product           string            `json:"product"`
	LineType          string            `json:"line_type"`
	Price             float64           `json:"price"`
	Unit              string            `json:"unit"`
	Quantity          float64           `json:"quantity"`
	OriginalAmount    float64           `json:"original_amount"`
	DiscountAmount    float64           `json:"discount_amount"`
	Amount            float64           `json:"amount"`
	Description       string            `json:"description"`
	TierDimensions    map[string]string `json:"tier_dimensions"`
	Resource          *costResource     `json:"resource"`
}

type costResource struct {
	ID          string           `json:"id"`
	DisplayName string           `json:"display_name"`
	Environment *costEnvironment `json:"environment"`
}

type costEnvironment struct {
	ID string `json:"id"`
}

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	confluentConfig, err := confluentplugin.GetConfluentConfig(configFile)
	if err != nil {
		log.Fatalf("error building Confluent config: %v", err)
	}
	log.SetLogLevel(confluentConfig.LogLevel)

	confluentCostSrc := ConfluentCostSource{
		client: http.DefaultClient,
		config: confluentConfig,
	}

	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &confluentCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

func (c *ConfluentCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
	results := []*pb.CustomCostResponse{}

	targets, err := opencost.GetWindows(req.Start.AsTime(), req.End.AsTime(), req.Resolution.AsDuration())
	if err != nil {
		return append(results, &pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting windows: %v", err)},
		})
	}

	if req.Resolution.AsDuration() != timeutil.Day {
		return append(results, &pb.CustomCostResponse{
			Errors: []string{"confluent plugin only supports daily resolution"},
		})
	}

	for _, target := range targets {
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		result := boilerplateConfluentCustomCost(target)
		costs, err := c.getCostsForWindow(target)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("error getting Confluent costs: %v", err))
			results = append(results, &result)
			continue
		}

		result.Costs = confluentCostsToCustomCosts(target, costs)
		results = append(results, &result)
	}

	return results
}

func boilerplateConfluentCustomCost(win opencost.Window) pb.CustomCostResponse {
	return pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "billing/v1"},
		CostSource: "SaaS",
		Domain:     "confluent",
		Version:    "billing/v1",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      []*pb.CustomCost{},
	}
}

func (c *ConfluentCostSource) getCostsForWindow(window opencost.Window) ([]costItem, error) {
	nextURL, err := c.costsURLForWindow(window)
	if err != nil {
		return nil, err
	}

	baseURL, err := url.Parse(strings.TrimRight(c.config.APIBaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid Confluent API base URL: %v", err)
	}

	costs := []costItem{}
	for nextURL != "" {
		resp, err := c.getCostsPage(nextURL, baseURL)
		if err != nil {
			return nil, err
		}

		costs = append(costs, resp.Data...)
		nextURL = strings.TrimSpace(resp.Metadata.Next)
	}

	return costs, nil
}

func (c *ConfluentCostSource) getCostsPage(endpoint string, baseURL *url.URL) (*costList, error) {
	parsedEndpoint, err := parseNextURL(endpoint, baseURL)
	if err != nil {
		return nil, err
	}
	if !sameAPIHost(parsedEndpoint, baseURL) {
		return nil, fmt.Errorf("refusing Confluent pagination URL with unexpected host: %s", parsedEndpoint.Host)
	}

	req, err := http.NewRequest(http.MethodGet, parsedEndpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating Confluent costs request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.config.APIKey, c.config.APISecret)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error doing Confluent costs request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		bodyString := "<empty>"
		if readErr == nil {
			bodyString = string(bodyBytes)
		}
		return nil, fmt.Errorf("received non-200 response for Confluent costs request: %d body: %s", resp.StatusCode, bodyString)
	}

	var page costList
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("error decoding Confluent costs response: %v", err)
	}

	return &page, nil
}

func (c *ConfluentCostSource) costsURLForWindow(window opencost.Window) (string, error) {
	baseURL, err := url.Parse(strings.TrimRight(c.config.APIBaseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("invalid Confluent API base URL: %v", err)
	}

	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/billing/v1/costs"
	query := baseURL.Query()
	query.Set("start_date", window.Start().UTC().Format(confluentDateFormat))
	query.Set("end_date", window.End().UTC().Format(confluentDateFormat))
	query.Set("page_size", fmt.Sprintf("%d", c.config.PageSize))
	baseURL.RawQuery = query.Encode()

	return baseURL.String(), nil
}

func confluentCostsToCustomCosts(window opencost.Window, costs []costItem) []*pb.CustomCost {
	customCosts := []*pb.CustomCost{}
	for _, item := range costs {
		resourceID, resourceName, environmentID := resourceFields(item)
		resourceType := firstNonEmpty(item.Product, item.LineType, "Confluent")
		resourceName = firstNonEmpty(resourceName, item.LineType, item.Product, item.ID, "Confluent cost")
		accountName := firstNonEmpty(environmentID, "confluent")
		chargeCategory := chargeCategory(item)

		providerIDParts := []string{item.ID, environmentID, resourceID, item.Product, item.LineType, item.StartDate, item.EndDate}
		providerID := strings.Join(compact(providerIDParts), "/")
		if providerID == "" {
			providerID = uuid.New().String()
		}

		pricingQuantity := float32(item.Quantity)
		effectiveCost := float32(item.Amount)
		provider := "Confluent"
		extendedAttrs := pb.CustomCostExtendedAttributes{
			BillingPeriodStart: timestamppb.New(parseCostDate(item.StartDate, *window.Start())),
			BillingPeriodEnd:   timestamppb.New(parseCostDate(item.EndDate, *window.End())),
			AccountId:          optionalString(environmentID),
			ChargeFrequency:    optionalString("Usage-Based"),
			EffectiveCost:      &effectiveCost,
			Provider:           &provider,
			Publisher:          &provider,
			ServiceCategory:    optionalString("SaaS"),
			ServiceName:        optionalString(item.Product),
			SkuId:              optionalString(item.LineType),
			SubAccountId:       optionalString(resourceID),
			SubAccountName:     optionalString(resourceName),
			PricingQuantity:    &pricingQuantity,
			PricingUnit:        optionalString(item.Unit),
			PricingCategory:    optionalString(item.NetworkAccessType),
		}

		customCost := pb.CustomCost{
			BilledCost:         float32(item.Amount),
			ListCost:           float32(item.OriginalAmount),
			AccountName:        accountName,
			ChargeCategory:     chargeCategory,
			Description:        costDescription(item),
			ResourceName:       resourceName,
			ResourceType:       resourceType,
			Id:                 uuid.New().String(),
			ProviderId:         providerID,
			UsageQuantity:      float32(item.Quantity),
			UsageUnit:          item.Unit,
			ExtendedAttributes: &extendedAttrs,
		}

		customCosts = append(customCosts, &customCost)
	}

	return customCosts
}

func resourceFields(item costItem) (string, string, string) {
	if item.Resource == nil {
		return "", "", ""
	}

	environmentID := ""
	if item.Resource.Environment != nil {
		environmentID = strings.TrimSpace(item.Resource.Environment.ID)
	}

	return strings.TrimSpace(item.Resource.ID), strings.TrimSpace(item.Resource.DisplayName), environmentID
}

func chargeCategory(item costItem) string {
	if strings.EqualFold(item.LineType, "PROMO_CREDIT") || item.Amount < 0 {
		return "Credit"
	}
	if strings.EqualFold(item.LineType, "SUPPORT") || strings.HasPrefix(strings.ToUpper(item.Product), "SUPPORT") {
		return "Support"
	}
	return "Usage"
}

func costDescription(item costItem) string {
	parts := compact([]string{"Confluent", item.Product, item.LineType})
	description := strings.Join(parts, " ")
	if item.Description != "" {
		description = strings.TrimSpace(description + " " + item.Description)
	}
	if description == "" {
		return "Confluent cost"
	}
	return description
}

func parseNextURL(endpoint string, baseURL *url.URL) (*url.URL, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid Confluent pagination URL: %v", err)
	}
	if parsed.IsAbs() {
		return parsed, nil
	}
	return baseURL.ResolveReference(parsed), nil
}

func sameAPIHost(candidate *url.URL, baseURL *url.URL) bool {
	return strings.EqualFold(candidate.Scheme, baseURL.Scheme) && strings.EqualFold(candidate.Host, baseURL.Host)
}

func parseCostDate(value string, fallback time.Time) time.Time {
	if value == "" {
		return fallback.UTC()
	}
	parsed, err := time.Parse(confluentDateFormat, value)
	if err != nil {
		return fallback.UTC()
	}
	return parsed.UTC()
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func compact(values []string) []string {
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

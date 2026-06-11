package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-plugin"
	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	coralogixplugin "github.com/opencost/opencost-plugins/pkg/plugins/coralogix/coralogixplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	coralogixDateTimeFormat = "2006-01-02T15:04:05.000Z"
	metricGBSent            = "gb_sent"
	metricUnitConsumption   = "unit_consumption"
)

var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "coralogix",
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type CoralogixCostSource struct {
	client HTTPClient
	config *coralogixplugin.CoralogixConfig
}

type dataUsageResponse struct {
	Entries []dataUsageEntry `json:"entries"`
}

type dataUsageEntry struct {
	Dimensions []dataUsageDimension `json:"dimensions"`
	SizeGB     float64              `json:"sizeGb"`
	Timestamp  string               `json:"timestamp"`
	Units      float64              `json:"units"`
}

type dataUsageDimension struct {
	GenericDimension *genericDimension `json:"genericDimension"`
	Pillar           string            `json:"pillar"`
	Priority         string            `json:"priority"`
	Severity         string            `json:"severity"`
	Tier             string            `json:"tier"`
}

type genericDimension struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type dimensionPair struct {
	Key   string
	Value string
}

type dataUsageMetric struct {
	Name        string
	DisplayName string
	Quantity    float64
	Unit        string
	UnitPrice   float64
}

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	coralogixConfig, err := coralogixplugin.GetCoralogixConfig(configFile)
	if err != nil {
		log.Fatalf("error building CoraLogix config: %v", err)
	}
	log.SetLogLevel(coralogixConfig.LogLevel)

	coralogixCostSrc := CoralogixCostSource{
		client: http.DefaultClient,
		config: coralogixConfig,
	}

	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &coralogixCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

func (c *CoralogixCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
	results := []*pb.CustomCostResponse{}

	targets, err := opencost.GetWindows(req.Start.AsTime(), req.End.AsTime(), req.Resolution.AsDuration())
	if err != nil {
		return append(results, &pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting windows: %v", err)},
		})
	}

	if req.Resolution.AsDuration() != timeutil.Day {
		return append(results, &pb.CustomCostResponse{
			Errors: []string{"coralogix plugin only supports daily resolution"},
		})
	}

	for _, target := range targets {
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		result := boilerplateCoralogixCustomCost(c.config, target)
		entries, err := c.getDataUsageForWindow(target)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("error getting CoraLogix data usage: %v", err))
			results = append(results, &result)
			continue
		}

		result.Costs = coralogixUsageToCustomCosts(c.config, entries, target)
		results = append(results, &result)
	}

	return results
}

func boilerplateCoralogixCustomCost(config *coralogixplugin.CoralogixConfig, win opencost.Window) pb.CustomCostResponse {
	return pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "data-usage/v2"},
		CostSource: "SaaS",
		Domain:     "coralogix",
		Version:    "data-usage/v2",
		Currency:   currency(config),
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      []*pb.CustomCost{},
	}
}

func (c *CoralogixCostSource) getDataUsageForWindow(window opencost.Window) ([]dataUsageEntry, error) {
	endpoint, err := c.dataUsageURLForWindow(window)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating CoraLogix data usage request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", c.config.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error doing CoraLogix data usage request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		bodyString := "<empty>"
		if readErr == nil {
			bodyString = string(bodyBytes)
		}
		return nil, fmt.Errorf("received non-200 response for CoraLogix data usage request: %d body: %s", resp.StatusCode, bodyString)
	}

	entries, err := parseDataUsageResponse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error parsing CoraLogix data usage response: %v", err)
	}

	return entries, nil
}

func (c *CoralogixCostSource) dataUsageURLForWindow(window opencost.Window) (string, error) {
	baseURL, err := url.Parse(strings.TrimRight(c.config.APIBaseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("invalid CoraLogix API base URL: %v", err)
	}

	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/dataplans/data-usage/v2"
	query := baseURL.Query()
	from := window.Start().UTC().Format(coralogixDateTimeFormat)
	to := window.End().UTC().Format(coralogixDateTimeFormat)
	setDateRangeQuery(query, c.config.DateRangeStyle, from, to)
	if strings.TrimSpace(c.config.Resolution) != "" {
		query.Set("resolution", c.config.Resolution)
	}
	for _, aggregate := range c.config.AggregateBy {
		query.Add("aggregate", aggregate)
	}
	baseURL.RawQuery = query.Encode()

	return baseURL.String(), nil
}

func setDateRangeQuery(query url.Values, style string, from string, to string) {
	switch style {
	case coralogixplugin.DateRangeStyleDeep:
		query.Set("date_range[fromDate]", from)
		query.Set("date_range[toDate]", to)
	case coralogixplugin.DateRangeStyleDot:
		query.Set("date_range.fromDate", from)
		query.Set("date_range.toDate", to)
	case coralogixplugin.DateRangeStyleJSON:
		encoded, _ := json.Marshal(map[string]string{
			"fromDate": from,
			"toDate":   to,
		})
		query.Set("date_range", string(encoded))
	default:
		query.Set("fromDate", from)
		query.Set("toDate", to)
	}
}

func parseDataUsageResponse(reader io.Reader) ([]dataUsageEntry, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return []dataUsageEntry{}, nil
	}

	if entries, ok, _ := decodeDataUsagePayload(body); ok {
		return entries, nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	entries := []dataUsageEntry{}
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		line = bytes.TrimPrefix(line, []byte("data:"))
		line = bytes.TrimSpace(line)
		if len(line) == 0 || bytes.Equal(line, []byte("[DONE]")) {
			continue
		}

		lineEntries, ok, err := decodeDataUsagePayload(line)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("unrecognized data usage stream line: %s", string(line))
		}
		entries = append(entries, lineEntries...)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func decodeDataUsagePayload(payload []byte) ([]dataUsageEntry, bool, error) {
	var response dataUsageResponse
	if err := json.Unmarshal(payload, &response); err == nil && response.Entries != nil {
		return response.Entries, true, nil
	}

	var entries []dataUsageEntry
	if err := json.Unmarshal(payload, &entries); err == nil {
		return entries, true, nil
	}

	var entry dataUsageEntry
	if err := json.Unmarshal(payload, &entry); err == nil && entryPopulated(entry) {
		return []dataUsageEntry{entry}, true, nil
	}

	var raw json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, false, err
	}
	return nil, false, nil
}

func entryPopulated(entry dataUsageEntry) bool {
	return len(entry.Dimensions) > 0 || entry.SizeGB != 0 || entry.Timestamp != "" || entry.Units != 0
}

func coralogixUsageToCustomCosts(config *coralogixplugin.CoralogixConfig, entries []dataUsageEntry, window opencost.Window) []*pb.CustomCost {
	customCosts := []*pb.CustomCost{}
	for _, entry := range entries {
		for _, metric := range metricsForEntry(config, entry) {
			if metric.Quantity == 0 {
				continue
			}

			dimensions := entryDimensionPairs(entry)
			dimensionLookup := dimensionMap(dimensions)
			resourceName := resourceName(dimensionLookup, dimensions)
			resourceType := firstNonEmpty(dimensionLookup["entity_type"], dimensionLookup["pillar"], metric.DisplayName)
			accountName := firstNonEmpty(dimensionLookup["application"], dimensionLookup["dataspace"], "coralogix")
			providerID := stableProviderID(entry, metric, dimensions)
			billedCost := float32(metric.Quantity * metric.UnitPrice)
			pricingQuantity := float32(metric.Quantity)
			provider := "CoraLogix"
			serviceName := "CoraLogix Data Usage"
			extendedAttrs := pb.CustomCostExtendedAttributes{
				BillingPeriodStart: timestamppb.New(parseEntryTimestamp(entry.Timestamp, *window.Start())),
				BillingPeriodEnd:   timestamppb.New(*window.End()),
				AccountId:          optionalString(accountName),
				ChargeFrequency:    optionalString("Usage-Based"),
				EffectiveCost:      &billedCost,
				Provider:           &provider,
				Publisher:          &provider,
				ServiceCategory:    optionalString("SaaS"),
				ServiceName:        optionalString(serviceName),
				SkuId:              optionalString(metric.Name),
				SubAccountName:     optionalString(resourceName),
				PricingQuantity:    &pricingQuantity,
				PricingUnit:        optionalString(metric.Unit),
				PricingCategory:    optionalString(resourceType),
			}

			customCost := pb.CustomCost{
				BilledCost:         billedCost,
				ListCost:           billedCost,
				AccountName:        accountName,
				ChargeCategory:     "Usage",
				Description:        costDescription(metric, dimensions),
				ResourceName:       resourceName,
				ResourceType:       resourceType,
				Id:                 uuid.New().String(),
				ProviderId:         providerID,
				UsageQuantity:      float32(metric.Quantity),
				UsageUnit:          metric.Unit,
				ExtendedAttributes: &extendedAttrs,
			}

			customCosts = append(customCosts, &customCost)
		}
	}

	return customCosts
}

func metricsForEntry(config *coralogixplugin.CoralogixConfig, entry dataUsageEntry) []dataUsageMetric {
	metrics := []dataUsageMetric{}
	if entry.SizeGB != 0 {
		metrics = append(metrics, dataUsageMetric{
			Name:        metricGBSent,
			DisplayName: "GB sent",
			Quantity:    entry.SizeGB,
			Unit:        "GB",
			UnitPrice:   priceForMetric(config, metricGBSent),
		})
	}
	if entry.Units != 0 {
		metrics = append(metrics, dataUsageMetric{
			Name:        metricUnitConsumption,
			DisplayName: "Unit consumption",
			Quantity:    entry.Units,
			Unit:        "unit",
			UnitPrice:   priceForMetric(config, metricUnitConsumption),
		})
	}
	return metrics
}

func priceForMetric(config *coralogixplugin.CoralogixConfig, metric string) float64 {
	if config == nil {
		return 0
	}
	if price, ok := config.MetricPrices[metric]; ok {
		return price
	}
	switch metric {
	case metricGBSent:
		return config.SizeGBUnitPrice
	case metricUnitConsumption:
		return config.UnitPrice
	default:
		return 0
	}
}

func entryDimensionPairs(entry dataUsageEntry) []dimensionPair {
	pairs := []dimensionPair{}
	for _, dimension := range entry.Dimensions {
		if dimension.GenericDimension != nil {
			key := strings.TrimSpace(dimension.GenericDimension.Key)
			value := strings.TrimSpace(dimension.GenericDimension.Value)
			if key != "" && value != "" {
				pairs = append(pairs, dimensionPair{Key: normalizeKey(key), Value: value})
			}
		}
		if value := enumDimensionValue(dimension.Pillar, "PILLAR_"); value != "" {
			pairs = append(pairs, dimensionPair{Key: "pillar", Value: value})
		}
		if value := enumDimensionValue(dimension.Priority, "PRIORITY_"); value != "" {
			pairs = append(pairs, dimensionPair{Key: "priority", Value: value})
		}
		if value := enumDimensionValue(dimension.Severity, "SEVERITY_"); value != "" {
			pairs = append(pairs, dimensionPair{Key: "severity", Value: value})
		}
		if value := enumDimensionValue(dimension.Tier, "TCO_TIER_"); value != "" {
			pairs = append(pairs, dimensionPair{Key: "tco_priority", Value: value})
		}
	}

	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].Key == pairs[j].Key {
			return pairs[i].Value < pairs[j].Value
		}
		return pairs[i].Key < pairs[j].Key
	})
	return pairs
}

func dimensionMap(pairs []dimensionPair) map[string]string {
	result := map[string]string{}
	for _, pair := range pairs {
		if _, exists := result[pair.Key]; !exists {
			result[pair.Key] = pair.Value
		}
	}
	return result
}

func normalizeKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "-", "_")
	return value
}

func enumDimensionValue(value string, prefix string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasSuffix(value, "_UNSPECIFIED") {
		return ""
	}
	value = strings.TrimPrefix(value, prefix)
	return strings.ToLower(value)
}

func resourceName(values map[string]string, dimensions []dimensionPair) string {
	application := values["application"]
	subsystem := values["subsystem"]
	if application != "" && subsystem != "" {
		return application + "/" + subsystem
	}
	if application != "" {
		return application
	}
	if subsystem != "" {
		return subsystem
	}
	if dataset := values["dataset"]; dataset != "" {
		return dataset
	}
	if dataspace := values["dataspace"]; dataspace != "" {
		return dataspace
	}
	if pillar := values["pillar"]; pillar != "" {
		return pillar
	}
	if len(dimensions) > 0 {
		return dimensions[0].Value
	}
	return "CoraLogix data usage"
}

func costDescription(metric dataUsageMetric, dimensions []dimensionPair) string {
	parts := []string{"CoraLogix", metric.DisplayName}
	dimensionsText := dimensionsDescription(dimensions)
	if dimensionsText != "" {
		parts = append(parts, dimensionsText)
	}
	return strings.Join(parts, " ")
}

func dimensionsDescription(dimensions []dimensionPair) string {
	parts := []string{}
	for _, dimension := range dimensions {
		parts = append(parts, dimension.Key+"="+dimension.Value)
	}
	return strings.Join(parts, ", ")
}

func stableProviderID(entry dataUsageEntry, metric dataUsageMetric, dimensions []dimensionPair) string {
	parts := compact([]string{
		"coralogix",
		metric.Name,
		entry.Timestamp,
		dimensionsDescription(dimensions),
		fmt.Sprintf("%.6f", metric.Quantity),
	})
	return strings.Join(parts, "/")
}

func parseEntryTimestamp(value string, fallback time.Time) time.Time {
	if value == "" {
		return fallback.UTC()
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return fallback.UTC()
	}
	return parsed.UTC()
}

func currency(config *coralogixplugin.CoralogixConfig) string {
	if config == nil || strings.TrimSpace(config.Currency) == "" {
		return coralogixplugin.DefaultCurrency
	}
	return config.Currency
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

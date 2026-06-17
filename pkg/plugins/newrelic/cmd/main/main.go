package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-plugin"
	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	newrelicplugin "github.com/opencost/opencost-plugins/pkg/plugins/newrelic/newrelicplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const newRelicNRQLTimeFormat = "2006-01-02 15:04:05 UTC"

var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "newrelic",
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type NewRelicCostSource struct {
	client HTTPClient
	config *newrelicplugin.NewRelicConfig
}

type nerdGraphResponse struct {
	Data   nerdGraphData    `json:"data"`
	Errors []nerdGraphError `json:"errors"`
}

type nerdGraphData struct {
	Actor nerdGraphActor `json:"actor"`
}

type nerdGraphActor struct {
	Account nerdGraphAccount `json:"account"`
}

type nerdGraphAccount struct {
	NRQL nerdGraphNRQL `json:"nrql"`
}

type nerdGraphNRQL struct {
	Results []map[string]interface{} `json:"results"`
}

type nerdGraphError struct {
	Message string `json:"message"`
}

type dimensionPair struct {
	Key   string
	Value string
}

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	newRelicConfig, err := newrelicplugin.GetNewRelicConfig(configFile)
	if err != nil {
		log.Fatalf("error building New Relic config: %v", err)
	}
	log.SetLogLevel(newRelicConfig.LogLevel)

	newRelicCostSrc := NewRelicCostSource{
		client: http.DefaultClient,
		config: newRelicConfig,
	}

	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &newRelicCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

func (n *NewRelicCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
	results := []*pb.CustomCostResponse{}

	targets, err := opencost.GetWindows(req.Start.AsTime(), req.End.AsTime(), req.Resolution.AsDuration())
	if err != nil {
		return append(results, &pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting windows: %v", err)},
		})
	}

	if req.Resolution.AsDuration() != timeutil.Day {
		return append(results, &pb.CustomCostResponse{
			Errors: []string{"newrelic plugin only supports daily resolution"},
		})
	}

	queries := usageQueries(n.config)
	for _, target := range targets {
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		result := boilerplateNewRelicCustomCost(n.config, target)
		for _, usageQuery := range queries {
			rows, err := n.getUsageForWindow(usageQuery, target)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("error getting New Relic %s usage: %v", usageQuery.Name, err))
				continue
			}
			result.Costs = append(result.Costs, newRelicUsageToCustomCosts(n.config, usageQuery, rows, target)...)
		}

		results = append(results, &result)
	}

	return results
}

func boilerplateNewRelicCustomCost(config *newrelicplugin.NewRelicConfig, win opencost.Window) pb.CustomCostResponse {
	return pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "nerdgraph"},
		CostSource: "SaaS",
		Domain:     "newrelic",
		Version:    "nerdgraph",
		Currency:   currency(config),
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      []*pb.CustomCost{},
	}
}

func usageQueries(config *newrelicplugin.NewRelicConfig) []newrelicplugin.UsageQueryConfig {
	if config != nil && len(config.UsageQueries) > 0 {
		return config.UsageQueries
	}
	return newrelicplugin.DefaultUsageQueries()
}

func (n *NewRelicCostSource) getUsageForWindow(usageQuery newrelicplugin.UsageQueryConfig, window opencost.Window) ([]map[string]interface{}, error) {
	nrql := renderNRQL(usageQuery.NRQL, window)
	payload := map[string]string{
		"query": graphqlNRQLQuery(n.config.AccountID, nrql),
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error encoding NerdGraph request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, n.config.NerdGraphURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("error creating NerdGraph request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("API-Key", n.config.APIKey)

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error doing NerdGraph request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		bodyString := "<empty>"
		if readErr == nil {
			bodyString = string(bodyBytes)
		}
		return nil, fmt.Errorf("received non-200 response for NerdGraph request: %d body: %s", resp.StatusCode, bodyString)
	}

	var graph nerdGraphResponse
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&graph); err != nil {
		return nil, fmt.Errorf("error decoding NerdGraph response: %v", err)
	}
	if len(graph.Errors) > 0 {
		return nil, fmt.Errorf("NerdGraph returned errors: %s", graphErrorMessages(graph.Errors))
	}

	return graph.Data.Actor.Account.NRQL.Results, nil
}

func renderNRQL(template string, window opencost.Window) string {
	start := window.Start().UTC()
	end := window.End().UTC()
	replacements := map[string]string{
		"{start}":         start.Format(newRelicNRQLTimeFormat),
		"{end}":           end.Format(newRelicNRQLTimeFormat),
		"{start_rfc3339}": start.Format(time.RFC3339),
		"{end_rfc3339}":   end.Format(time.RFC3339),
		"{start_unix}":    fmt.Sprintf("%d", start.Unix()),
		"{end_unix}":      fmt.Sprintf("%d", end.Unix()),
	}

	rendered := template
	for placeholder, value := range replacements {
		rendered = strings.ReplaceAll(rendered, placeholder, value)
	}
	return rendered
}

func graphqlNRQLQuery(accountID int, nrql string) string {
	return fmt.Sprintf(`{ actor { account(id: %d) { nrql(query: %q) { results } } } }`, accountID, nrql)
}

func graphErrorMessages(errors []nerdGraphError) string {
	messages := []string{}
	for _, err := range errors {
		message := strings.TrimSpace(err.Message)
		if message != "" {
			messages = append(messages, message)
		}
	}
	if len(messages) == 0 {
		return "<unknown>"
	}
	return strings.Join(messages, "; ")
}

func newRelicUsageToCustomCosts(config *newrelicplugin.NewRelicConfig, usageQuery newrelicplugin.UsageQueryConfig, rows []map[string]interface{}, window opencost.Window) []*pb.CustomCost {
	customCosts := []*pb.CustomCost{}
	for _, row := range rows {
		quantity, ok := quantityFromRow(row, usageQuery.QuantityAttribute)
		if !ok || quantity == 0 {
			continue
		}

		dimensions := rowDimensionPairs(row, usageQuery)
		dimensionLookup := dimensionMap(dimensions)
		accountName := firstNonEmpty(dimensionLookup["consumingAccountName"], dimensionLookup["consumingAccountId"], config.AccountName)
		resourceName := resourceName(usageQuery, dimensionLookup, dimensions)
		resourceType := firstNonEmpty(usageQuery.ResourceType, dimensionLookup["metric"], dimensionLookup["productLine"], usageQuery.Name)
		unit := firstNonEmpty(usageQuery.Unit, "unit")
		unitPrice := priceForQuery(config, usageQuery)
		billedCost := float32(quantity * unitPrice)
		pricingQuantity := float32(quantity)
		provider := "New Relic"
		serviceName := "New Relic Usage"
		billingStart := timestampFromRow(row, "beginTimeSeconds", *window.Start())
		billingEnd := timestampFromRow(row, "endTimeSeconds", *window.End())

		extendedAttrs := pb.CustomCostExtendedAttributes{
			BillingPeriodStart: timestamppb.New(billingStart),
			BillingPeriodEnd:   timestamppb.New(billingEnd),
			AccountId:          optionalString(accountName),
			ChargeFrequency:    optionalString("Usage-Based"),
			EffectiveCost:      &billedCost,
			Provider:           &provider,
			Publisher:          &provider,
			ServiceCategory:    optionalString("SaaS"),
			ServiceName:        optionalString(serviceName),
			SkuId:              optionalString(firstNonEmpty(usageQuery.PriceKey, usageQuery.Name)),
			SubAccountName:     optionalString(resourceName),
			PricingQuantity:    &pricingQuantity,
			PricingUnit:        optionalString(unit),
			PricingCategory:    optionalString(resourceType),
		}

		customCost := pb.CustomCost{
			BilledCost:         billedCost,
			ListCost:           billedCost,
			AccountName:        accountName,
			ChargeCategory:     "Usage",
			Description:        costDescription(usageQuery, dimensions),
			ResourceName:       resourceName,
			ResourceType:       resourceType,
			Id:                 uuid.New().String(),
			ProviderId:         stableProviderID(config, usageQuery, dimensions, quantity, window),
			UsageQuantity:      float32(quantity),
			UsageUnit:          unit,
			ExtendedAttributes: &extendedAttrs,
		}

		customCosts = append(customCosts, &customCost)
	}

	return customCosts
}

func quantityFromRow(row map[string]interface{}, quantityAttribute string) (float64, bool) {
	if quantityAttribute == "" {
		quantityAttribute = newrelicplugin.DefaultQuantityAttribute
	}
	if value, ok := row[quantityAttribute]; ok {
		return numberValue(value)
	}
	if quantityAttribute != newrelicplugin.DefaultQuantityAttribute {
		if value, ok := row[newrelicplugin.DefaultQuantityAttribute]; ok {
			return numberValue(value)
		}
	}

	keys := make([]string, 0, len(row))
	for key := range row {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if isMetadataKey(key) {
			continue
		}
		if value, ok := numberValue(row[key]); ok {
			return value, true
		}
	}
	return 0, false
}

func isMetadataKey(key string) bool {
	switch key {
	case "beginTimeSeconds", "endTimeSeconds", "timestamp", "facet":
		return true
	default:
		return false
	}
}

func rowDimensionPairs(row map[string]interface{}, usageQuery newrelicplugin.UsageQueryConfig) []dimensionPair {
	pairs := []dimensionPair{}
	addPair := func(key string, value interface{}) {
		key = strings.TrimSpace(key)
		if key == "" {
			key = "facet"
		}
		valueString := stringValue(value)
		if valueString == "" {
			return
		}
		for _, pair := range pairs {
			if pair.Key == key && pair.Value == valueString {
				return
			}
		}
		pairs = append(pairs, dimensionPair{Key: key, Value: valueString})
	}

	if facet, ok := row["facet"]; ok {
		switch typed := facet.(type) {
		case []interface{}:
			for i, value := range typed {
				addPair(facetAttributeName(usageQuery.FacetAttributes, i), value)
			}
		default:
			addPair(facetAttributeName(usageQuery.FacetAttributes, 0), typed)
		}
	}

	for _, attribute := range usageQuery.FacetAttributes {
		addPair(attribute, row[attribute])
	}

	for _, attribute := range knownDimensionAttributes() {
		addPair(attribute, row[attribute])
	}

	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].Key == pairs[j].Key {
			return pairs[i].Value < pairs[j].Value
		}
		return pairs[i].Key < pairs[j].Key
	})

	return pairs
}

func facetAttributeName(attributes []string, index int) string {
	if index >= 0 && index < len(attributes) {
		return attributes[index]
	}
	return fmt.Sprintf("facet_%d", index+1)
}

func knownDimensionAttributes() []string {
	return []string{
		"consumingAccountId",
		"consumingAccountName",
		"dimension_productCapability",
		"metric",
		"productLine",
		"syntheticsLocationLabel",
		"syntheticsMonitorName",
		"syntheticsTypeLabel",
		"usageMetric",
	}
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

func resourceName(usageQuery newrelicplugin.UsageQueryConfig, values map[string]string, dimensions []dimensionPair) string {
	if monitor := values["syntheticsMonitorName"]; monitor != "" {
		return monitor
	}
	if usageMetric := values["usageMetric"]; usageMetric != "" {
		return usageMetric
	}
	if capability := values["dimension_productCapability"]; capability != "" {
		return capability
	}
	if syntheticsType := values["syntheticsTypeLabel"]; syntheticsType != "" {
		return syntheticsType
	}
	if metric := values["metric"]; metric != "" {
		return metric
	}
	if len(dimensions) > 0 {
		return dimensions[0].Value
	}
	return usageQuery.Name
}

func costDescription(usageQuery newrelicplugin.UsageQueryConfig, dimensions []dimensionPair) string {
	parts := []string{"New Relic", firstNonEmpty(usageQuery.ResourceType, usageQuery.Name), "usage"}
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

func stableProviderID(config *newrelicplugin.NewRelicConfig, usageQuery newrelicplugin.UsageQueryConfig, dimensions []dimensionPair, quantity float64, window opencost.Window) string {
	accountID := ""
	if config != nil {
		accountID = fmt.Sprintf("%d", config.AccountID)
	}
	parts := compact([]string{
		"newrelic",
		accountID,
		usageQuery.Name,
		window.Start().UTC().Format("2006-01-02"),
		dimensionsDescription(dimensions),
		fmt.Sprintf("%.6f", quantity),
	})
	return strings.Join(parts, "/")
}

func priceForQuery(config *newrelicplugin.NewRelicConfig, usageQuery newrelicplugin.UsageQueryConfig) float64 {
	if config == nil {
		return 0
	}
	if usageQuery.UnitPrice != 0 {
		return usageQuery.UnitPrice
	}
	priceKey := firstNonEmpty(usageQuery.PriceKey, usageQuery.Name)
	if price, ok := config.MetricPrices[priceKey]; ok {
		return price
	}
	switch priceKey {
	case newrelicplugin.MetricDataIngest:
		return config.GigabytesIngestedUnitPrice
	case newrelicplugin.MetricCoreCCU:
		return config.CoreCCUUnitPrice
	case newrelicplugin.MetricAdvancedCCU:
		return config.AdvancedCCUUnitPrice
	case newrelicplugin.MetricSyntheticChecks:
		return config.SyntheticCheckUnitPrice
	default:
		return 0
	}
}

func timestampFromRow(row map[string]interface{}, key string, fallback time.Time) time.Time {
	value, ok := row[key]
	if !ok {
		return fallback.UTC()
	}
	number, ok := numberValue(value)
	if !ok {
		return fallback.UTC()
	}
	if number > 1e12 {
		return time.UnixMilli(int64(number)).UTC()
	}
	return time.Unix(int64(number), 0).UTC()
}

func numberValue(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case string:
		typed = strings.TrimSpace(strings.TrimPrefix(typed, "$"))
		typed = strings.ReplaceAll(typed, ",", "")
		if typed == "" {
			return 0, false
		}
		number, err := strconv.ParseFloat(typed, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func stringValue(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%g", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	default:
		return ""
	}
}

func currency(config *newrelicplugin.NewRelicConfig) string {
	if config == nil || strings.TrimSpace(config.Currency) == "" {
		return newrelicplugin.DefaultCurrency
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

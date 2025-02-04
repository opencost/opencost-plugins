package main

import (
	"context"
	"encoding/json"
	"fmt"
	_nethttp "net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"
	"github.com/agnivade/levenshtein"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"
	"github.com/hashicorp/go-plugin"
	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	datadogplugin "github.com/opencost/opencost-plugins/pkg/plugins/datadog/datadogplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
)

// URL of the Datadog pricing page
const url = "https://aws.amazon.com/marketplace/pp/prodview-536p4hpqbajc2"

// handshakeConfigs are used to just do a basic handshake between
// a plugin and host. If the handshake fails, a user friendly error is shown.
// This prevents users from executing bad plugins or executing a plugin
// directory. It is a UX feature, not a security feature.
var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "datadog",
}

// Implementation of CustomCostSource
type DatadogCostSource struct {
	ddCtx       context.Context
	usageApi    *datadogV2.UsageMeteringApi
	v1UsageApi  *datadogV1.UsageMeteringApi
	rateLimiter *rate.Limiter
}

func (d *DatadogCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
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
		// Call the function to scrape prices
		unitPricing, err := d.GetDDUnitPrices(target.Start().UTC())
		if err != nil {
			log.Errorf("error getting dd pricing: %v", err)
			errResp := pb.CustomCostResponse{
				Errors: []string{fmt.Sprintf("error getting dd pricing: %v", err)},
			}
			results = append(results, &errResp)
			return results
		} else {
			log.Debugf("got unit pricing: %v", unitPricing)
		}
		// DataDog gets mad if we ask them to tell the future
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		log.Debugf("fetching DD costs for window %v", target)
		result := d.getDDCostsForWindow(target, unitPricing)
		results = append(results, result)
	}

	return results
}

func main() {

	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	ddConfig, err := getDatadogConfig(configFile)
	if err != nil {
		log.Fatalf("error building DD config: %v", err)
	}
	log.SetLogLevel(ddConfig.DDLogLevel)
	// datadog usage APIs allow 10 requests every 30 seconds
	rateLimiter := rate.NewLimiter(0.1, 1)
	ddCostSrc := DatadogCostSource{
		rateLimiter: rateLimiter,
	}
	ddCostSrc.ddCtx, ddCostSrc.usageApi, ddCostSrc.v1UsageApi = getDatadogClients(*ddConfig)

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &ddCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

func boilerplateDDCustomCost(win opencost.Window) pb.CustomCostResponse {
	return pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v2"},
		CostSource: "observability",
		Domain:     "datadog",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      []*pb.CustomCost{},
	}
}
func (d *DatadogCostSource) getDDCostsForWindow(window opencost.Window, listPricing map[string]billableCost) *pb.CustomCostResponse {
	ccResp := boilerplateDDCustomCost(window)
	costs := map[string]*pb.CustomCost{}
	nextPageId := "init"
	for morepages := true; morepages; morepages = (nextPageId != "") {
		params := datadogV2.NewGetHourlyUsageOptionalParameters()
		if nextPageId != "init" {
			params.PageNextRecordId = &nextPageId
		}

		if d.rateLimiter.Tokens() < 1.0 {
			log.Infof("datadog rate limit reached. holding request until rate capacity is back")
		}

		err := d.rateLimiter.WaitN(context.TODO(), 1)
		if err != nil {
			log.Errorf("error waiting on rate limiter`: %v\n", err)
			ccResp.Errors = append(ccResp.Errors, err.Error())
			return &ccResp
		}

		maxTries := 5
		try := 1
		var resp datadogV2.HourlyUsageResponse
		for try <= maxTries {
			params.FilterTimestampEnd = window.End()
			var r *_nethttp.Response
			resp, r, err = d.usageApi.GetHourlyUsage(d.ddCtx, *window.Start(), "all", *params)
			if err != nil {
				log.Errorf("Error when calling `UsageMeteringApi.GetHourlyUsage`: %v\n", err)
				log.Errorf("Full HTTP response: %v\n", r)
			}

			if err == nil {
				break
			} else {
				if strings.Contains(err.Error(), "429") {
					log.Errorf("rate limit reached, retrying...")
				}
				time.Sleep(30 * time.Second)
				try++
			}

		}

		if err != nil {
			log.Errorf("after calling `UsageMeteringApi.GetHourlyUsage` %d times, still getting error: %v\n", maxTries, err)
			ccResp.Errors = append(ccResp.Errors, err.Error())
		}

		for index := range resp.Data {
			// each of these entries gives hourly data steps
			for indexMeas := range resp.Data[index].Attributes.Measurements {
				usageQty := float32(0.0)

				if resp.Data[index].Attributes.Measurements[indexMeas].Value.IsSet() {
					usageQty = float32(resp.Data[index].Attributes.Measurements[indexMeas].GetValue())
				}

				if usageQty == 0.0 {
					log.Tracef("product %s/%s had 0 usage, not recording that cost", *resp.Data[index].Attributes.ProductFamily, *resp.Data[index].Attributes.Measurements[indexMeas].UsageType)
					continue
				}

				matched, pricing := matchUsageToPricing(*resp.Data[index].Attributes.Measurements[indexMeas].UsageType, listPricing)
				log.Infof("matched %s to %s", *resp.Data[index].Attributes.Measurements[indexMeas].UsageType, matched)
				provId := *resp.Data[index].Attributes.PublicId + "/" + *resp.Data[index].Attributes.Measurements[indexMeas].UsageType
				if matched == "" {
					log.Infof("no pricing found for %s", *resp.Data[index].Attributes.Measurements[indexMeas].UsageType)
					continue
				}

				billedCost := float32(pricing.Cost) * usageQty

				if _, found := costs[provId]; found {
					// we have already encountered this cost type for this window, so add to the existing cost entry
					costs[provId].UsageQuantity += usageQty
					costs[provId].BilledCost += billedCost
				} else {
					// we have not encountered this cost type for this window yet, so create a new cost entry
					cost := pb.CustomCost{
						Zone:               *resp.Data[index].Attributes.Region,
						AccountName:        *resp.Data[index].Attributes.OrgName,
						ChargeCategory:     "usage",
						Description:        "nil",
						ResourceName:       *resp.Data[index].Attributes.Measurements[indexMeas].UsageType,
						ResourceType:       *resp.Data[index].Attributes.ProductFamily,
						Id:                 *resp.Data[index].Id,
						ProviderId:         provId,
						Labels:             map[string]string{},
						ListCost:           0,
						ListUnitPrice:      0,
						BilledCost:         billedCost,
						UsageQuantity:      usageQty,
						UsageUnit:          pricing.unit,
						ExtendedAttributes: nil,
					}

					costs[provId] = &cost
				}
			}
		}
		if resp.Meta != nil && resp.Meta.Pagination != nil && resp.Meta.Pagination.NextRecordId.IsSet() {
			nextPageId = *resp.Meta.Pagination.NextRecordId.Get()
		} else {
			nextPageId = ""
		}
	}
	allCosts := []*pb.CustomCost{}
	for _, cost := range costs {
		allCosts = append(allCosts, cost)
	}
	ccResp.Costs = allCosts

	// post processing
	// datadog's usage API sometimes provides usages that get counted multiple times
	// this post processing stage de-duplicates those usages and costs
	postProcess(&ccResp)

	return &ccResp
}

func matchUsageToPricing(usageType string, pricing map[string]billableCost) (string, *billableCost) {
	// for the usage, remove _count from the end of the usage type
	usageType = strings.TrimSuffix(usageType, "_count")

	// not specific enough to match on
	if usageType == "host" {
		return "", nil
	}
	// if the usage type is in the pricing map, use that
	if _, found := pricing[usageType]; found {
		entry := pricing[usageType]
		return usageType, &entry
	}

	// break up the usage on _
	tokens := strings.Split(usageType, "_")
	// find the first pricing key that contains all tokens
	for key, price := range pricing {
		matchesAll := true
		for _, token := range tokens {
			if !strings.Contains(key, token) {
				matchesAll = false
				break
			}
		}
		if matchesAll {
			return key, &price
		}
	}

	// try replacing agent with infra and checking that
	agentAsInfra := strings.ReplaceAll(usageType, "agent", "infra")
	tokens = strings.Split(agentAsInfra, "_")
	// find the first pricing key that contains all tokens
	for key, price := range pricing {
		matchesAll := true
		for _, token := range tokens {
			if !strings.Contains(key, token) {
				matchesAll = false
				break
			}
		}
		if matchesAll {
			return key, &price
		}
	}

	// if still no pricing key is found, compute the levenshtein distance between the usage type and the pricing key
	// and use the one with the smallest distance
	smallestDist := 4000000000
	var closestKey string
	for key := range pricing {
		distance := levenshtein.ComputeDistance(usageType, key)
		if distance < smallestDist {
			smallestDist = distance
			closestKey = key
		}
	}

	// remember the pricing keys we have already matched. if we have already matched a pricing key, don't match it again
	entry := pricing[closestKey]
	return closestKey, &entry

}
func postProcess(ccResp *pb.CustomCostResponse) {
	if ccResp == nil {
		return
	}

	ccResp.Costs = processInfraHosts(ccResp.Costs)

	ccResp.Costs = processLogUsage(ccResp.Costs)

	// DBM queries have 200 * number of hosts included. We need to adjust the costs to reflect this
	ccResp.Costs = adjustDBMQueries(ccResp.Costs)

	// removes any items that have 0 usage, either because of post processing or otherwise
	ccResp.Costs = removeZeroUsages(ccResp.Costs)
}

// as per https://www.datadoghq.com/pricing/?product=database-monitoring#database-monitoring-can-i-still-use-dbm-if-i-have-additional-normalized-queries-past-the-a-hrefpricingallotmentsallotteda-amount
// the first 200 queries per host are free.
// if that zeroes out the dbm queries, we remove the cost
func adjustDBMQueries(costs []*pb.CustomCost) []*pb.CustomCost {
	totalFreeQueries := float32(0.0)
	for index := 0; index < len(costs); index++ {
		if costs[index].ResourceName == "dbm_host_count" {
			hostCount := costs[index].UsageQuantity
			totalFreeQueries += 200 * float32(hostCount)
		}
	}
	log.Debugf("total free queries: %f", totalFreeQueries)

	for index := 0; index < len(costs); index++ {
		if costs[index].ResourceName == "dbm_queries_count" {
			costs[index].UsageQuantity -= totalFreeQueries
			log.Debugf("adjusted dbm queries: %v", costs[index])
		}

	}

	for index := 0; index < len(costs); index++ {
		if costs[index].ResourceName == "dbm_queries_count" {
			if costs[index].UsageQuantity <= 0 {
				log.Debugf("removing cost %s because it has 0 usage", costs[index].ProviderId)
				costs = append(costs[:index], costs[index+1:]...)
				index = 0
			} else {
				// TODO else, multiply cost by the rate for extra queries
				costs[index].ListCost = 0.0
				costs[index].ListUnitPrice = 0.0
				costs[index].UsageUnit = "queries"
			}
		}
	}
	return costs
}

// removes any items that have 0 usage or cost, either because of post processing or otherwise
func removeZeroUsages(costs []*pb.CustomCost) []*pb.CustomCost {
	log.Tracef("POST -costs length before post processing: %d", len(costs))
	for index := 0; index < len(costs); index++ {
		log.Tracef("POST - looking at cost %s with usage %f", costs[index].ResourceName, costs[index].UsageQuantity)
		if costs[index].UsageQuantity < 0.001 && costs[index].ListCost == 0.0 && costs[index].BilledCost == 0.0 {
			if costs[index].ResourceName == "dbm_queries_count" {
				log.Tracef("leaving dbm queries cost in place")
				continue
			}
			log.Tracef("POST -removing cost %s because it has 0 usage", costs[index].ProviderId)
			costs = append(costs[:index], costs[index+1:]...)
			log.Tracef("POST - costs is now %d", len(costs))
			index = -1
		}
	}
	log.Tracef("POST -costs length after post processing: %d", len(costs))

	return costs
}

func processInfraHosts(costs []*pb.CustomCost) []*pb.CustomCost {
	// remove the container_count item
	for index := 0; index < len(costs); index++ {
		if costs[index].ResourceName == "container_count" {
			costs = append(costs[:index], costs[index+1:]...)
			index = 0
		}
	}

	return costs
}

func processLogUsage(costs []*pb.CustomCost) []*pb.CustomCost {

	for index := range costs {
		if costs[index].ResourceName == "logs_live_indexed_events_15_day_count" {
			costs = append(costs[:index], costs[index+1:]...)
			break
		}
	}

	// remove live indexed events count, that is covered by the other categories
	for index := range costs {
		if costs[index].ResourceName == "logs_live_indexed_count" {
			costs = append(costs[:index], costs[index+1:]...)
			break
		}
	}

	var logsIndexed *pb.CustomCost
	for index := range costs {
		if costs[index].ResourceName == "indexed_events_count" {
			logsIndexed = costs[index]
			costs = append(costs[:index], costs[index+1:]...)
			break
		}
	}

	if logsIndexed != nil {
		leftoverLogs := float32(0.0)
		for index := range costs {
			if costs[index].ResourceName == "logs_indexed_events_15_day_count" {
				leftoverLogs = float32(logsIndexed.UsageQuantity) - float32(costs[index].UsageQuantity)
				break
			}
		}

		logsIndexed.Description = "other log events"
		logsIndexed.UsageQuantity = leftoverLogs
		logsIndexed.ResourceName = "other_log_events"
		logsIndexed.ListCost = leftoverLogs * logsIndexed.ListUnitPrice
		costs = append(costs, logsIndexed)
	}
	return costs
}

func getDatadogClients(config datadogplugin.DatadogConfig) (context.Context, *datadogV2.UsageMeteringApi, *datadogV1.UsageMeteringApi) {
	ddctx := datadog.NewDefaultContext(context.Background())
	ddctx = context.WithValue(
		ddctx,
		datadog.ContextServerVariables,
		map[string]string{"site": config.DDSite},
	)

	keys := make(map[string]datadog.APIKey)

	keys["apiKeyAuth"] = datadog.APIKey{Key: config.DDAPIKey}
	keys["appKeyAuth"] = datadog.APIKey{Key: config.DDAppKey}

	ddctx = context.WithValue(
		ddctx,
		datadog.ContextAPIKeys,
		keys,
	)

	configuration := datadog.NewConfiguration()
	apiClient := datadog.NewAPIClient(configuration)
	usageAPI := datadogV2.NewUsageMeteringApi(apiClient)
	v1UsageAPI := datadogV1.NewUsageMeteringApi(apiClient)
	return ddctx, usageAPI, v1UsageAPI
}

func getDatadogConfig(configFilePath string) (*datadogplugin.DatadogConfig, error) {
	var result datadogplugin.DatadogConfig
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file for DD config @ %s: %v", configFilePath, err)
	}
	err = json.Unmarshal(bytes, &result)
	if err != nil {
		return nil, fmt.Errorf("error marshaling json into DD config %v", err)
	}

	if result.DDLogLevel == "" {
		result.DDLogLevel = "info"
	}

	return &result, nil
}

func (d *DatadogCostSource) GetDDUnitPrices(windowStart time.Time) (map[string]billableCost, error) {

	// DD estimated costs can be delayed 72 hours
	// so ensure we are going far enough back
	stableTimeframe := time.Now().UTC().Add(-3 * 24 * time.Hour)

	targetMonth := time.Date(stableTimeframe.Year(), stableTimeframe.Month(), 1, 0, 0, 0, 0, time.UTC)
	targetMonthEnd := time.Date(stableTimeframe.Year(), stableTimeframe.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	// first, get the billable usage for the month
	opts := datadogV1.GetUsageBillableSummaryOptionalParameters{
		Month: &targetMonth,
	}
	var respBillableUsage datadogV1.UsageBillableSummaryResponse
	var err error
	for try := 1; try <= 5; {
		respBillableUsage, _, err = d.v1UsageApi.GetUsageBillableSummary(d.ddCtx, opts)
		if err == nil {
			break
		} else {
			if strings.Contains(err.Error(), "429") {
				log.Errorf("rate limit reached, retrying...")
			} else {
				break
			}
			time.Sleep(30 * time.Second)
			try++
		}

	}
	if err != nil {
		return nil, fmt.Errorf("error getting usage billable usage summary: %v", err)
	}
	// then, get the estimated cost for the month
	// the start date should be the beginning of the month
	// the end date should be the end of the last month, or the stable time frame, depending on if we are in the first 3 days of the new month or not
	endDateToUse := targetMonthEnd
	if time.Now().Before(targetMonthEnd) {
		endDateToUse = stableTimeframe
	}

	costOpts := datadogV2.GetEstimatedCostByOrgOptionalParameters{
		StartDate: &targetMonth,
		EndDate:   &endDateToUse,
	}
	var respEstimatedCost datadogV2.CostByOrgResponse
	for try := 1; try <= 5; {
		respEstimatedCost, _, err = d.usageApi.GetEstimatedCostByOrg(d.ddCtx, costOpts)

		if err == nil {
			break
		} else {
			if strings.Contains(err.Error(), "429") {
				log.Errorf("rate limit reached, retrying...")
			} else {
				break
			}
			time.Sleep(30 * time.Second)
			try++
		}
	}

	if err != nil {
		return nil, fmt.Errorf("after calling `UsageMeteringApi.GetEstimatedCostByOrg` %d times, still getting error: %v", 5, err)
	}

	// now, we need to calculate the unit prices
	// the unit price is the estimated cost divided by the billable usage
	// we need to do this for each product family
	costsByFamily := make(map[string]float64)
	latestCosts := respEstimatedCost.Data[len(respEstimatedCost.Data)-1]
	attrs := latestCosts.Attributes
	for _, charge := range attrs.Charges {
		if *charge.ChargeType != "total" {
			continue
		}
		costsByFamily[*charge.ProductName] = float64(*charge.Cost)
	}

	result := make(map[string]billableCost)
	for _, usage := range respBillableUsage.Usage {
		log.Debugf("usage: %v", usage)
		for productName, cost := range costsByFamily {
			usageAmount, unit := GetAccountBillableUsage(productName, usage.Usage)
			if usageAmount == 0 {
				continue
			}
			// if the product family has 'hosts' in it, then the usage is per month
			// so we need to adjust the cost to be per hour
			isRated := false
			if strings.Contains(productName, "host") {
				isRated = true
				cost /= float64(730)
			}

			result[productName] = billableCost{
				ProductName: productName,
				Cost:        cost / float64(usageAmount),
				isRated:     isRated,
				unit:        unit,
			}
		}
	}

	return result, nil
}

type billableCost struct {
	ProductName string
	Cost        float64
	isRated     bool
	unit        string
}

// CheckAccountBillableUsage checks if any AccountBillableUsage equals one.
func GetAccountBillableUsage(billingDimension string, o *datadogV1.UsageBillableSummaryKeys) (int64, string) {
	v := reflect.ValueOf(o).Elem()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.Kind() == reflect.Ptr && !field.IsNil() {
			usage := field.Interface().(*datadogV1.UsageBillableSummaryBody)
			if len(usage.AdditionalProperties) > 0 {
				if usage.AdditionalProperties["billing_dimension"] == billingDimension {
					return *usage.AccountBillableUsage, *usage.UsageUnit
				}
			}
		}
	}

	// if not in the reflected fields, check the AdditionalProperties
	for name, usage := range o.AdditionalProperties {
		if strings.Contains(name, billingDimension) {
			untypedUsage := usage.(map[string]interface{})
			untyped := untypedUsage["account_billable_usage"]
			typed := int64(untyped.(float64))
			return typed, untypedUsage["usage_unit"].(string)
		}
	}
	log.Warnf("no AccountBillableUsage found for billing dimension %s", billingDimension)
	return 0, ""
}

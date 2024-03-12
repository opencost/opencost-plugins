package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"os"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/hashicorp/go-plugin"
	datadogplugin "github.com/opencost/opencost-plugins/datadog/datadogplugin"
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

	// Call the function to scrape prices
	listPricing, err := scrapeDatadogPrices(url)
	if err != nil {
		log.Errorf("error getting dd pricing: %v", err)
		errResp := pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting dd pricing: %v", err)},
		}
		results = append(results, &errResp)
		return results
	} else {
		log.Debugf("got list pricing: %v", listPricing.Details)
	}

	for _, target := range targets {
		log.Debugf("fetching DD costs for window %v", target)
		result := d.getDDCostsForWindow(target, listPricing)
		results = append(results, result)
	}

	return results
}

func main() {

	configFile, err := getConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	ddConfig, err := getDatadogConfig(configFile)
	if err != nil {
		log.Fatalf("error building DD config: %v", err)
	}
	log.SetLogLevel(ddConfig.DDLogLevel)
	// datadog usage APIs allow 10 requests every 30 seconds
	rateLimiter := rate.NewLimiter(0.25, 5)
	ddCostSrc := DatadogCostSource{
		rateLimiter: rateLimiter,
	}
	ddCostSrc.ddCtx, ddCostSrc.usageApi = getDatadogClients(*ddConfig)

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
func (d *DatadogCostSource) getDDCostsForWindow(window opencost.Window, listPricing *datadogplugin.PricingInformation) *pb.CustomCostResponse {
	ccResp := boilerplateDDCustomCost(window)

	nextPageId := "init"
	nextPagePrevPeriodId := "init"
	for morepages := true; morepages; morepages = (nextPageId != "") {
		params := datadogV2.NewGetHourlyUsageOptionalParameters()
		if nextPageId != "init" {
			params.PageNextRecordId = &nextPageId
		}

		if d.rateLimiter.Tokens() < 1.0 {
			log.Infof("datadog rate limit reached. holding request until rate capacity is back")
		}

		err := d.rateLimiter.WaitN(context.TODO(), 2)
		if err != nil {
			log.Errorf("error waiting on rate limiter`: %v\n", err)
			ccResp.Errors = append(ccResp.Errors, err.Error())
			return &ccResp
		}

		params.FilterTimestampEnd = window.End()
		resp, r, err := d.usageApi.GetHourlyUsage(d.ddCtx, *window.Start(), "all", *params)
		if err != nil {
			log.Errorf("Error when calling `UsageMeteringApi.GetHourlyUsage`: %v\n", err)
			log.Errorf("Full HTTP response: %v\n", r)
			ccResp.Errors = append(ccResp.Errors, err.Error())
		}

		// many datadog usages are given in terms of a cumulative month to date usage
		// therefore, make a call for the hour before this hour to get a comparison
		// where needed
		params.FilterTimestampEnd = window.Start()
		toSub := window.End().Sub(*window.Start())
		if nextPagePrevPeriodId != "init" {
			params.PageNextRecordId = &nextPagePrevPeriodId
		}
		respPriorWindow, r, err := d.usageApi.GetHourlyUsage(d.ddCtx, (*window.Start()).Add(-toSub), "all", *params)
		if err != nil {
			log.Errorf("Error when calling `UsageMeteringApi.GetHourlyUsage`: %v\n", err)
			log.Errorf("Full HTTP response: %v\n", r)
			ccResp.Errors = append(ccResp.Errors, err.Error())
		}

		for index := range resp.Data {
			for indexMeas := range resp.Data[index].Attributes.Measurements {
				usageQty := float32(0.0)

				if resp.Data[index].Attributes.Measurements[indexMeas].Value.IsSet() {
					var prior *datadogV2.HourlyUsageMeasurement
					if len(respPriorWindow.Data) > index {
						log.Infof("getting prior window data from timeframe %v, and measurement %v", window, resp.Data[index].Attributes.Measurements[indexMeas])
						log.Infof("prior window data: %v", respPriorWindow.Data[index])
						prior = &respPriorWindow.Data[index].Attributes.Measurements[indexMeas]
					} else {
						// then this is an out of bound access
						log.Warnf("could not get prior window data from timeframe %v, and measurement %v", window, resp.Data[index].Attributes.Measurements[indexMeas])
						log.Warnf("passing in nil prior window data")
					}
					usageQty = GetUsageQuantity(*resp.Data[index].Attributes.ProductFamily, &resp.Data[index].Attributes.Measurements[indexMeas], prior)
				}

				if usageQty == 0.0 {
					log.Tracef("product %s/%s had 0 usage, not recording that cost", *resp.Data[index].Attributes.ProductFamily, *resp.Data[index].Attributes.Measurements[indexMeas].UsageType)
					continue
				}

				desc, usageUnit, pricing, currency := getListingInfo(window, *resp.Data[index].Attributes.ProductFamily, *resp.Data[index].Attributes.Measurements[indexMeas].UsageType, listPricing)
				ccResp.Currency = currency
				cost := pb.CustomCost{
					Zone:               *resp.Data[index].Attributes.Region,
					AccountName:        *resp.Data[index].Attributes.OrgName,
					ChargeCategory:     "usage",
					Description:        desc,
					ResourceName:       *resp.Data[index].Attributes.Measurements[indexMeas].UsageType,
					ResourceType:       *resp.Data[index].Attributes.ProductFamily,
					Id:                 *resp.Data[index].Id,
					ProviderId:         *resp.Data[index].Attributes.PublicId + "/" + *resp.Data[index].Attributes.Measurements[indexMeas].UsageType,
					Labels:             map[string]string{},
					ListCost:           usageQty * pricing,
					ListUnitPrice:      pricing,
					UsageQuantity:      usageQty,
					UsageUnit:          usageUnit,
					ExtendedAttributes: nil,
				}
				ccResp.Costs = append(ccResp.Costs, &cost)
			}
		}
		if resp.Meta != nil && resp.Meta.Pagination != nil && resp.Meta.Pagination.NextRecordId.IsSet() {
			nextPageId = *resp.Meta.Pagination.NextRecordId.Get()
		} else {
			nextPageId = ""
		}

		if respPriorWindow.Meta != nil && respPriorWindow.Meta.Pagination != nil && respPriorWindow.Meta.Pagination.NextRecordId.IsSet() {
			nextPagePrevPeriodId = *respPriorWindow.Meta.Pagination.NextRecordId.Get()
		} else {
			nextPagePrevPeriodId = ""
		}
	}

	return &ccResp
}

// we have two basic types usages: cumulative and rate
// rate usages are e.g., number of infra hosts, that have fixed costs per hour
// cumulative usages are e.g., number of logs ingested, that have a fixed cost per unit
// if a usage is cumulative, then suptract the usage in the hour prior to get the incremental usage
// if a usage is a rate, then just return the usage
func GetUsageQuantity(productFamily string, currentPeriodUsage, previousPeriodUsage *datadogV2.HourlyUsageMeasurement) float32 {
	curUsage := currentPeriodUsage.GetValue()
	if _, found := rateFamilies[productFamily]; found {
		// this family is a rate family, so just return the usage
		return float32(curUsage)
	}

	prevUsage := int64(0)
	if previousPeriodUsage == nil {
		log.Warnf("previous period usage was nil, assuming 0 usage for that timeframe for family %s", productFamily)
	} else {
		prevUsage = previousPeriodUsage.GetValue()
	}

	return float32(curUsage - prevUsage)
}

// the public pricing used in the pricing list doesn't always match the usage reports
// therefore, we maintain a list of aliases
var usageToPricingMap = map[string]string{
	"timeseries": "custom_metrics",

	"apm_uncategorized_host_count":     "apm_hosts",
	"apm_host_count_incl_usm":          "apm_hosts",
	"apm_azure_app_service_host_count": "apm_hosts",
	"apm_devsecops_host_count":         "apm_hosts",
	"apm_host_count":                   "apm_hosts",
	"opentelemetry_apm_host_count":     "apm_hosts",
	"apm_fargate_count":                "apm_hosts",

	"container_count":                "containers",
	"container_count_excl_agent":     "containers",
	"billable_ingested_bytes":        "ingested_logs",
	"ingested_events_bytes":          "ingested_logs",
	"logs_live_ingested_bytes":       "ingested_logs",
	"logs_rehydrated_ingested_bytes": "ingested_logs",
	"indexed_events_count":           "indexed_logs",
	"logs_live_indexed_count":        "indexed_logs",
	"synthetics_api":                 "api_tests",
	"synthetics_browser":             "browser_checks",
	"tasks_count":                    "fargate_tasks",
	"rum":                            "rum_events",
	"analyzed_logs":                  "security_logs",
	"snmp":                           "snmp_device",
	"invocations_sum":                "serverless_inv",
}

var pricingMap = map[string]float64{
	"custom_metrics": 100.0,
	"indexed_logs":   1000000.0,
	"ingested_logs":  1024.0 * 1024.0 * 1024.0 * 1024.0,
	"api_tests":      10000.0,
	"browser_checks": 1000.0,
	"rum_events":     10000.0,
	"security_logs":  1024.0 * 1024.0 * 1024.0 * 1024.0,
	"serverless_inv": 1000000.0,
}

var rateFamilies = map[string]int{
	"infra_hosts": 730.0,
	"apm_hosts":   730.0,
}

func getListingInfo(window opencost.Window, productfamily string, usageType string, listPricing *datadogplugin.PricingInformation) (description string, usageUnit string, pricing float32, currency string) {
	pricingKey := ""
	var found bool
	// first, check if the usage type is mapped to a pricing key
	if pricingKey, found = usageToPricingMap[usageType]; found {
		log.Debugf("usage type %s was mapped to pricing key %s", usageType, pricingKey)
	} else if pricingKey, found = usageToPricingMap[productfamily]; found {
		// if it isn't then check if the family is mapped to a pricing key
		log.Debugf("product family %s was mapped to pricing key %s", productfamily, pricingKey)
	} else {
		// if it isn't, then the family is the pricing key
		pricingKey = productfamily
	}

	matchedPrice := false
	// search through the pricing for the right key
	for _, detail := range listPricing.Details {
		if pricingKey == detail.Name {
			matchedPrice = true
			description = detail.DetailDescription
			usageUnit = detail.Units
			currency = detail.OneMonths.Currency
			pricingFloat, err := strconv.ParseFloat(detail.OneMonths.Rate, 32)
			if err != nil {
				log.Errorf("error converting string to float for rate: %s", detail.OneMonths.Rate)
			}

			// if the family is a rate family, then the pricing is per hour
			if hourlyPriceDenominator, found := rateFamilies[pricingKey]; found {
				// adjust the pricing to fit the window duration
				pricingPerHour := float32(pricingFloat) / float32(hourlyPriceDenominator)
				pricing = pricingPerHour * float32(window.Duration().Hours())
			} else {
				// if the family is a cumulative family, then the pricing is per unit
				// check for a scale factor on the pricing
				if scalefactor, found := pricingMap[pricingKey]; found {
					pricing = float32(pricingFloat) / float32(scalefactor)
				} else {
					pricing = float32(pricingFloat)
				}
			}

		}
	}

	if !matchedPrice {
		log.Warnf("unable to find pricing for product %s/%s. going to set to 0 price", productfamily, usageType)
		usageType = "PRICING UNAVAILABLE"
		description = productfamily + " " + usageType
		pricing = 0.0
		currency = ""
	}
	// return the data from the usage entry
	return
}

func getDatadogClients(config datadogplugin.DatadogConfig) (context.Context, *datadogV2.UsageMeteringApi) {
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
	return ddctx, usageAPI
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

func getConfigFilePath() (string, error) {
	// plugins expect exactly 2 args: the executable itself,
	// and a path to the config file to use
	// all config for the plugin must come through the config file
	if len(os.Args) != 2 {
		return "", fmt.Errorf("plugins require 2 args: the plugin itself, and the full path to its config file. Got %d args", len(os.Args))
	}

	_, err := os.Stat(os.Args[1])
	if err != nil {
		return "", fmt.Errorf("error reading config file at %s: %v", os.Args[1], err)
	}

	return os.Args[1], nil
}

func scrapeDatadogPrices(url string) (*datadogplugin.PricingInformation, error) {
	// Send a GET request to the URL
	response, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch the page: %v", err)
	}
	defer response.Body.Close()

	// Check if the request was successful
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to retrieve pricing page. Status code: %d", response.StatusCode)
	}
	b, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read pricing page body: %v", err)
	}
	res := datadogplugin.DatadogProJSON{}
	r := regexp.MustCompile(`var productDetailData = \s*(.*?)\s*;`)
	log.Tracef("got response: %s", string(b))
	matches := r.FindAllStringSubmatch(string(b), -1)
	if len(matches) != 1 {
		return nil, fmt.Errorf("requires exactly 1 product detail data, got %d", len(matches))
	}

	log.Tracef("matches[0][1]:" + matches[0][1])
	err = json.Unmarshal([]byte(matches[0][1]), &res)
	if err != nil {
		return nil, fmt.Errorf("failed to read pricing page body: %v", err)
	}

	return &res.OfferData.PricingInformation, nil
}

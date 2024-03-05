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

	"github.com/hashicorp/go-plugin"
	datadogplugin "github.com/opencost/opencost-plugins/datadog/datadogplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model"
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

func (d *DatadogCostSource) GetCustomCosts(req model.CustomCostRequestInterface) []model.CustomCostResponse {
	results := []model.CustomCostResponse{}

	targets, err := opencost.GetWindows(*req.GetTargetWindow().Start(), *req.GetTargetWindow().End(), req.GetTargetResolution())
	if err != nil {
		log.Errorf("error getting windows: %v", err)
		errResp := model.CustomCostResponse{
			Errors: []error{err},
		}
		results = append(results, errResp)
		return results
	}

	// Call the function to scrape prices
	listPricing, err := scrapeDatadogPrices(url)
	if err != nil {
		log.Errorf("error getting dd pricing: %v", err)
		errResp := model.CustomCostResponse{
			Errors: []error{err},
		}
		results = append(results, errResp)
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
	})
}

func boilerplateDDCustomCost(win opencost.Window) model.CustomCostResponse {
	return model.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v2"},
		Costsource: "observability",
		Domain:     "datadog",
		Version:    "v1",
		Currency:   "USD",
		Window:     win,
		Errors:     []error{},
		Costs:      []*model.CustomCost{},
	}
}
func (d *DatadogCostSource) getDDCostsForWindow(window opencost.Window, listPricing *datadogplugin.PricingInformation) model.CustomCostResponse {
	ccResp := boilerplateDDCustomCost(window)
	params := datadogV2.NewGetHourlyUsageOptionalParameters()
	params.FilterTimestampEnd = window.End()

	nextPageId := "init"
	for morepages := true; morepages; morepages = (nextPageId != "") {
		if nextPageId != "init" {
			params.PageNextRecordId = &nextPageId
		}
		if d.rateLimiter.Tokens() < 1.0 {
			log.Infof("datadog rate limit reached. holding request until rate capacity is back")
		}

		err := d.rateLimiter.Wait(context.TODO())
		if err != nil {
			log.Errorf("error waiting on rate limiter`: %v\n", err)
			ccResp.Errors = append(ccResp.Errors, err)
			return ccResp
		}

		
		resp, r, err := d.usageApi.GetHourlyUsage(d.ddCtx, *window.Start(), "all", *params)
		if err != nil {
			log.Errorf("Error when calling `UsageMeteringApi.GetHourlyUsage`: %v\n", err)
			log.Errorf("Full HTTP response: %v\n", r)
			ccResp.Errors = append(ccResp.Errors, err)
		}

		for _, hourlyUsageData := range resp.Data {
			for _, meas := range hourlyUsageData.Attributes.Measurements {
				clonedPtr := window.Clone()
				usageQty := float32(0.0)

				if meas.Value.IsSet() {
					usageQty = float32(meas.GetValue())
				}

				if usageQty == 0.0 {
					log.Tracef("product %s/%s had 0 usage, not recording that cost", *hourlyUsageData.Attributes.ProductFamily, *meas.UsageType)
					continue
				}

				desc, usageUnit, pricing, currency := getListingInfo(*hourlyUsageData.Attributes.ProductFamily, *meas.UsageType, listPricing)
				ccResp.Currency = currency
				cost := model.CustomCost{
					Zone:               *hourlyUsageData.Attributes.Region,
					AccountName:        *hourlyUsageData.Attributes.OrgName,
					ChargeCategory:     "usage",
					Description:        desc,
					ResourceName:       *meas.UsageType,
					ResourceType:       *hourlyUsageData.Attributes.ProductFamily,
					Id:                 *hourlyUsageData.Id,
					ProviderId:         *hourlyUsageData.Attributes.PublicId + "/" + *meas.UsageType,
					Window:             &clonedPtr,
					Labels:             map[string]string{},
					ListCost:           usageQty * pricing,
					ListUnitPrice:      pricing,
					UsageQty:           usageQty,
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
	}

	return ccResp
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
	"synthetics_api":                 "api_tests",
	"synthetics_browser":             "browser_checks",
	"tasks_count":                    "fargate_tasks",
	"rum":                            "rum_events",
	"analyzed_logs":                  "security_logs",
	"snmp":                           "snmp_device",
	"invocations_sum":                "serverless_inv",
}

func getListingInfo(productfamily string, usageType string, listPricing *datadogplugin.PricingInformation) (description string, usageUnit string, pricing float32, currency string) {
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
			pricing = float32(pricingFloat)
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
		return nil, fmt.Errorf("Failed to fetch the page: %v", err)
	}
	defer response.Body.Close()

	// Check if the request was successful
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Failed to retrieve pricing page. Status code: %d", response.StatusCode)
	}
	b, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("Failed to read pricing page body: %v", err)
	}
	res := datadogplugin.DatadogProJSON{}
	r := regexp.MustCompile(`var productDetailData = \s*(.*?)\s*;`)
	log.Debugf("got response: %s", string(b))
	matches := r.FindAllStringSubmatch(string(b), -1)
	if len(matches) != 1 {
		return nil, fmt.Errorf("requires exactly 1 product detail data, got %d", len(matches))
	}

	log.Debugf("matches[0][1]:" + matches[0][1])
	err = json.Unmarshal([]byte(matches[0][1]), &res)
	if err != nil {
		return nil, fmt.Errorf("Failed to read pricing page body: %v", err)
	}

	return &res.OfferData.PricingInformation, nil
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-plugin"
	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	cloudamqpplugin "github.com/opencost/opencost-plugins/pkg/plugins/cloudamqp/cloudamqpplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const cloudAMQPMonthFormat = "2006-01"

var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "cloudamqp",
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type CloudAMQPCostSource struct {
	client HTTPClient
	config *cloudamqpplugin.CloudAMQPConfig
}

type invoiceResponse struct {
	Period   string                   `json:"period"`
	Total    float64                  `json:"total"`
	Currency string                   `json:"currency"`
	Lines    []map[string]interface{} `json:"lines"`
	Customer map[string]interface{}   `json:"customer"`
	Seller   map[string]interface{}   `json:"seller"`
}

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	cloudAMQPConfig, err := cloudamqpplugin.GetCloudAMQPConfig(configFile)
	if err != nil {
		log.Fatalf("error building CloudAMQP config: %v", err)
	}
	log.SetLogLevel(cloudAMQPConfig.LogLevel)

	cloudAMQPCostSrc := CloudAMQPCostSource{
		client: http.DefaultClient,
		config: cloudAMQPConfig,
	}

	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &cloudAMQPCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

func (c *CloudAMQPCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
	results := []*pb.CustomCostResponse{}

	targets, err := opencost.GetWindows(req.Start.AsTime(), req.End.AsTime(), req.Resolution.AsDuration())
	if err != nil {
		return append(results, &pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting windows: %v", err)},
		})
	}

	if req.Resolution.AsDuration() != timeutil.Day {
		return append(results, &pb.CustomCostResponse{
			Errors: []string{"cloudamqp plugin only supports daily resolution"},
		})
	}

	invoiceCache := map[string]*invoiceResponse{}
	for _, target := range targets {
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		result := boilerplateCloudAMQPCustomCost(target, c.config.Currency)
		month := target.Start().UTC().Format(cloudAMQPMonthFormat)
		invoice, ok := invoiceCache[month]
		if !ok {
			invoice, err = c.getInvoiceForMonth(target.Start().UTC())
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("error getting CloudAMQP invoice: %v", err))
				results = append(results, &result)
				continue
			}
			invoiceCache[month] = invoice
		}

		if strings.TrimSpace(invoice.Currency) != "" {
			result.Currency = invoice.Currency
		}
		result.Costs = cloudAMQPInvoiceToCustomCosts(c.config, invoice, target)
		results = append(results, &result)
	}

	return results
}

func boilerplateCloudAMQPCustomCost(win opencost.Window, currency string) pb.CustomCostResponse {
	return pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "1.0"},
		CostSource: "SaaS",
		Domain:     "cloudamqp",
		Version:    "1.0",
		Currency:   currency,
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      []*pb.CustomCost{},
	}
}

func (c *CloudAMQPCostSource) getInvoiceForMonth(target time.Time) (*invoiceResponse, error) {
	endpoint, err := c.invoiceURLForMonth(target)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating CloudAMQP invoice request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth("", c.config.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error doing CloudAMQP invoice request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		bodyString := "<empty>"
		if readErr == nil {
			bodyString = string(bodyBytes)
		}
		return nil, fmt.Errorf("received non-200 response for CloudAMQP invoice request: %d body: %s", resp.StatusCode, bodyString)
	}

	var invoice invoiceResponse
	if err := json.NewDecoder(resp.Body).Decode(&invoice); err != nil {
		return nil, fmt.Errorf("error decoding CloudAMQP invoice response: %v", err)
	}

	return &invoice, nil
}

func (c *CloudAMQPCostSource) invoiceURLForMonth(target time.Time) (string, error) {
	baseURL, err := url.Parse(strings.TrimRight(c.config.APIBaseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("invalid CloudAMQP API base URL: %v", err)
	}

	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/invoices/period"
	query := baseURL.Query()
	query.Set("year", fmt.Sprintf("%d", target.Year()))
	query.Set("month", fmt.Sprintf("%d", int(target.Month())))
	baseURL.RawQuery = query.Encode()

	return baseURL.String(), nil
}

func cloudAMQPInvoiceToCustomCosts(config *cloudamqpplugin.CloudAMQPConfig, invoice *invoiceResponse, window opencost.Window) []*pb.CustomCost {
	if invoice == nil {
		return []*pb.CustomCost{}
	}

	period := firstNonEmpty(invoice.Period, window.Start().UTC().Format(cloudAMQPMonthFormat))
	days := daysInMonth(*window.Start())
	accountName := firstNonEmpty(stringFromMap(invoice.Customer, "name", "account_name", "company"), config.AccountName)
	currency := firstNonEmpty(invoice.Currency, config.Currency)

	if len(invoice.Lines) == 0 {
		return []*pb.CustomCost{cloudAMQPCustomCostForLine(config, invoice, nil, 0, invoice.Total/float64(days), invoice.Total, accountName, currency, period, window)}
	}

	customCosts := []*pb.CustomCost{}
	for i, line := range invoice.Lines {
		total, ok := amountFromLine(line)
		if !ok {
			continue
		}
		customCosts = append(customCosts, cloudAMQPCustomCostForLine(config, invoice, line, i, total/float64(days), total, accountName, currency, period, window))
	}

	if len(customCosts) == 0 && invoice.Total != 0 {
		customCosts = append(customCosts, cloudAMQPCustomCostForLine(config, invoice, nil, 0, invoice.Total/float64(days), invoice.Total, accountName, currency, period, window))
	}

	return customCosts
}

func cloudAMQPCustomCostForLine(config *cloudamqpplugin.CloudAMQPConfig, invoice *invoiceResponse, line map[string]interface{}, index int, dailyAmount float64, monthlyAmount float64, accountName string, currency string, period string, window opencost.Window) *pb.CustomCost {
	lineID := firstNonEmpty(stringFromMap(line, "id", "line_id"), fmt.Sprintf("%d", index))
	description := firstNonEmpty(stringFromMap(line, "description", "name", "title", "plan", "product"), "CloudAMQP invoice")
	resourceName := firstNonEmpty(stringFromMap(line, "instance_name", "instance", "name", "description"), description)
	resourceType := firstNonEmpty(stringFromMap(line, "plan", "product", "type"), "CloudAMQP")
	usageQuantity := float32(firstNonZeroFloat(numberFromMap(line, "quantity", "qty"), 1/float64(daysInMonth(*window.Start()))))
	usageUnit := firstNonEmpty(stringFromMap(line, "unit", "unit_name"), "month-prorated-day")
	billedCost := float32(dailyAmount)
	provider := "CloudAMQP"
	providerID := strings.Join(compact([]string{period, lineID, window.Start().UTC().Format("2006-01-02")}), "/")
	if providerID == "" {
		providerID = uuid.New().String()
	}

	extendedAttrs := pb.CustomCostExtendedAttributes{
		BillingPeriodStart: timestamppb.New(monthStart(*window.Start())),
		BillingPeriodEnd:   timestamppb.New(monthEndExclusive(*window.Start())),
		AccountId:          optionalString(accountName),
		ChargeFrequency:    optionalString("Monthly"),
		EffectiveCost:      &billedCost,
		Provider:           &provider,
		Publisher:          &provider,
		ServiceCategory:    optionalString("SaaS"),
		ServiceName:        optionalString("Managed RabbitMQ"),
		SkuId:              optionalString(resourceType),
		SubAccountName:     optionalString(resourceName),
		PricingQuantity:    &usageQuantity,
		PricingUnit:        optionalString(usageUnit),
	}

	return &pb.CustomCost{
		BilledCost:         billedCost,
		ListCost:           billedCost,
		AccountName:        accountName,
		ChargeCategory:     "Usage",
		Description:        fmt.Sprintf("%s prorated from %s monthly invoice total %.2f %s", description, period, monthlyAmount, currency),
		ResourceName:       resourceName,
		ResourceType:       resourceType,
		Id:                 uuid.New().String(),
		ProviderId:         providerID,
		UsageQuantity:      usageQuantity,
		UsageUnit:          usageUnit,
		ExtendedAttributes: &extendedAttrs,
	}
}

func amountFromLine(line map[string]interface{}) (float64, bool) {
	for _, key := range []string{"amount", "total", "subtotal", "price", "cost"} {
		value, ok := line[key]
		if !ok {
			continue
		}
		amount, ok := numberValue(value)
		if ok {
			return amount, true
		}
	}
	return 0, false
}

func numberFromMap(values map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if number, ok := numberValue(value); ok {
				return number
			}
		}
	}
	return 0
}

func numberValue(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
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

func stringFromMap(values map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if str := stringValue(value); str != "" {
				return str
			}
		}
	}
	return ""
}

func stringValue(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if math.Trunc(typed) == typed {
			return fmt.Sprintf("%.0f", typed)
		}
		return fmt.Sprintf("%f", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case bool:
		return fmt.Sprintf("%t", typed)
	default:
		return ""
	}
}

func firstNonZeroFloat(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func daysInMonth(target time.Time) int {
	return time.Date(target.UTC().Year(), target.UTC().Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func monthStart(target time.Time) time.Time {
	return time.Date(target.UTC().Year(), target.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
}

func monthEndExclusive(target time.Time) time.Time {
	return monthStart(target).AddDate(0, 1, 0)
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

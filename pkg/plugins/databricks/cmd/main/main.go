package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-plugin"
	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	databricksplugin "github.com/opencost/opencost-plugins/pkg/plugins/databricks/databricksplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	databricksDateFormat  = "2006-01-02"
	databricksMonthFormat = "2006-01"
)

var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "databricks",
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type DatabricksCostSource struct {
	client HTTPClient
	config *databricksplugin.DatabricksConfig
}

type usageRecord struct {
	RecordID             string
	AccountID            string
	WorkspaceID          string
	WorkspaceName        string
	SKUName              string
	Cloud                string
	UsageStartTime       string
	UsageEndTime         string
	UsageDate            string
	UsageUnit            string
	UsageQuantity        float64
	RecordType           string
	BillingOriginProduct string
	UsageType            string
	ClusterID            string
	JobID                string
	WarehouseID          string
	InstancePoolID       string
}

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	databricksConfig, err := databricksplugin.GetDatabricksConfig(configFile)
	if err != nil {
		log.Fatalf("error building Databricks config: %v", err)
	}
	log.SetLogLevel(databricksConfig.LogLevel)

	databricksCostSrc := DatabricksCostSource{
		client: http.DefaultClient,
		config: databricksConfig,
	}

	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &databricksCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

func (d *DatabricksCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
	results := []*pb.CustomCostResponse{}

	targets, err := opencost.GetWindows(req.Start.AsTime(), req.End.AsTime(), req.Resolution.AsDuration())
	if err != nil {
		return append(results, &pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting windows: %v", err)},
		})
	}

	if req.Resolution.AsDuration() != timeutil.Day {
		return append(results, &pb.CustomCostResponse{
			Errors: []string{"databricks plugin only supports daily resolution"},
		})
	}

	monthCache := map[string][]usageRecord{}
	for _, target := range targets {
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		result := boilerplateDatabricksCustomCost(target)
		month := target.Start().UTC().Format(databricksMonthFormat)
		records, ok := monthCache[month]
		if !ok {
			records, err = d.getUsageForMonth(month)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("error getting Databricks usage: %v", err))
				results = append(results, &result)
				continue
			}
			monthCache[month] = records
		}

		result.Costs = databricksUsageToCustomCosts(d.config, recordsForWindow(records, target), target)
		results = append(results, &result)
	}

	return results
}

func boilerplateDatabricksCustomCost(win opencost.Window) pb.CustomCostResponse {
	return pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "2.0"},
		CostSource: "SaaS",
		Domain:     "databricks",
		Version:    "2.0",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      []*pb.CustomCost{},
	}
}

func (d *DatabricksCostSource) getUsageForMonth(month string) ([]usageRecord, error) {
	endpoint, err := d.usageDownloadURL(month)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating Databricks usage request: %v", err)
	}
	req.Header.Set("Accept", "text/plain")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", d.config.Token))

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error doing Databricks usage request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		bodyString := "<empty>"
		if readErr == nil {
			bodyString = string(bodyBytes)
		}
		return nil, fmt.Errorf("received non-200 response for Databricks usage request: %d body: %s", resp.StatusCode, bodyString)
	}

	records, err := parseUsageCSV(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error parsing Databricks usage CSV: %v", err)
	}

	return records, nil
}

func (d *DatabricksCostSource) usageDownloadURL(month string) (string, error) {
	baseURL, err := url.Parse(strings.TrimRight(d.config.AccountAPIBaseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("invalid Databricks account API base URL: %v", err)
	}

	baseURL.Path = fmt.Sprintf("%s/api/2.0/accounts/%s/usage/download", strings.TrimRight(baseURL.Path, "/"), url.PathEscape(d.config.AccountID))
	query := baseURL.Query()
	query.Set("start_month", month)
	query.Set("end_month", month)
	if d.config.IncludePersonalData {
		query.Set("personal_data", "true")
	}
	baseURL.RawQuery = query.Encode()

	return baseURL.String(), nil
}

func parseUsageCSV(reader io.Reader) ([]usageRecord, error) {
	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1

	rows, err := csvReader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []usageRecord{}, nil
	}

	headerIndex := buildHeaderIndex(rows[0])
	records := []usageRecord{}
	for rowNumber, row := range rows[1:] {
		quantity, err := parseFloatField(getCSVField(row, headerIndex, "usage_quantity", "dbus", "dbu"))
		if err != nil {
			return nil, fmt.Errorf("row %d usage quantity: %v", rowNumber+2, err)
		}

		record := usageRecord{
			RecordID:             getCSVField(row, headerIndex, "record_id", "recordid"),
			AccountID:            getCSVField(row, headerIndex, "account_id", "accountid"),
			WorkspaceID:          getCSVField(row, headerIndex, "workspace_id", "workspaceid"),
			WorkspaceName:        getCSVField(row, headerIndex, "workspace_name", "workspacename"),
			SKUName:              getCSVField(row, headerIndex, "sku_name", "skuname", "sku"),
			Cloud:                getCSVField(row, headerIndex, "cloud"),
			UsageStartTime:       getCSVField(row, headerIndex, "usage_start_time", "usagestarttime", "start_time", "starttime", "timestamp"),
			UsageEndTime:         getCSVField(row, headerIndex, "usage_end_time", "usageendtime", "end_time", "endtime"),
			UsageDate:            getCSVField(row, headerIndex, "usage_date", "usagedate", "date"),
			UsageUnit:            getCSVField(row, headerIndex, "usage_unit", "usageunit", "unit"),
			UsageQuantity:        quantity,
			RecordType:           getCSVField(row, headerIndex, "record_type", "recordtype"),
			BillingOriginProduct: getCSVField(row, headerIndex, "billing_origin_product", "billingoriginproduct", "product"),
			UsageType:            getCSVField(row, headerIndex, "usage_type", "usagetype"),
			ClusterID:            getCSVField(row, headerIndex, "usage_metadata.cluster_id", "cluster_id", "clusterid"),
			JobID:                getCSVField(row, headerIndex, "usage_metadata.job_id", "job_id", "jobid"),
			WarehouseID:          getCSVField(row, headerIndex, "usage_metadata.warehouse_id", "warehouse_id", "warehouseid"),
			InstancePoolID:       getCSVField(row, headerIndex, "usage_metadata.instance_pool_id", "instance_pool_id", "instancepoolid"),
		}
		records = append(records, record)
	}

	return records, nil
}

func buildHeaderIndex(headers []string) map[string]int {
	result := map[string]int{}
	for i, header := range headers {
		result[normalizeHeader(header)] = i
	}
	return result
}

func getCSVField(row []string, headerIndex map[string]int, names ...string) string {
	for _, name := range names {
		if idx, ok := headerIndex[normalizeHeader(name)]; ok && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
	}
	return ""
}

func normalizeHeader(header string) string {
	header = strings.ToLower(strings.TrimSpace(header))
	replacer := strings.NewReplacer("_", "", "-", "", " ", "", ".", "")
	return replacer.Replace(header)
}

func parseFloatField(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	value = strings.TrimPrefix(value, "$")
	value = strings.ReplaceAll(value, ",", "")
	return strconv.ParseFloat(value, 64)
}

func recordsForWindow(records []usageRecord, window opencost.Window) []usageRecord {
	result := []usageRecord{}
	for _, record := range records {
		if usageRecordInWindow(record, window) {
			result = append(result, record)
		}
	}
	return result
}

func usageRecordInWindow(record usageRecord, window opencost.Window) bool {
	if record.UsageDate != "" {
		usageDate, err := time.Parse(databricksDateFormat, record.UsageDate)
		if err == nil {
			return usageDate.Equal(time.Date(window.Start().UTC().Year(), window.Start().UTC().Month(), window.Start().UTC().Day(), 0, 0, 0, 0, time.UTC))
		}
	}

	usageStart, err := parseDatabricksTime(record.UsageStartTime)
	if err != nil {
		return false
	}

	return !usageStart.Before(window.Start().UTC()) && usageStart.Before(window.End().UTC())
}

func databricksUsageToCustomCosts(config *databricksplugin.DatabricksConfig, records []usageRecord, window opencost.Window) []*pb.CustomCost {
	customCosts := []*pb.CustomCost{}
	for _, record := range records {
		unitPrice := unitPriceForSKU(config, record.SKUName)
		billedCost := float32(record.UsageQuantity * unitPrice)
		usageQuantity := float32(record.UsageQuantity)
		provider := "Databricks"
		resourceID := firstNonEmpty(record.ClusterID, record.JobID, record.WarehouseID, record.InstancePoolID, record.WorkspaceID)
		resourceName := firstNonEmpty(resourceID, record.WorkspaceName, record.SKUName, "Databricks usage")
		serviceName := firstNonEmpty(record.BillingOriginProduct, record.UsageType, record.SKUName, "Databricks")
		providerID := firstNonEmpty(record.RecordID, strings.Join(compact([]string{record.AccountID, record.WorkspaceID, record.SKUName, record.UsageStartTime, record.UsageDate}), "/"), uuid.New().String())
		chargeCategory := "Usage"
		if strings.EqualFold(record.RecordType, "RETRACTION") || record.UsageQuantity < 0 {
			chargeCategory = "Adjustment"
		}

		extendedAttrs := pb.CustomCostExtendedAttributes{
			BillingPeriodStart: timestamppb.New(parseDatabricksTimeOrFallback(record.UsageStartTime, *window.Start())),
			BillingPeriodEnd:   timestamppb.New(parseDatabricksTimeOrFallback(record.UsageEndTime, *window.End())),
			AccountId:          optionalString(firstNonEmpty(record.AccountID, config.AccountID)),
			ChargeFrequency:    optionalString("Usage-Based"),
			Subcategory:        optionalString(record.UsageType),
			EffectiveCost:      &billedCost,
			Provider:           &provider,
			Publisher:          &provider,
			ServiceCategory:    optionalString("SaaS"),
			ServiceName:        optionalString(serviceName),
			SkuId:              optionalString(record.SKUName),
			SubAccountId:       optionalString(record.WorkspaceID),
			SubAccountName:     optionalString(record.WorkspaceName),
			PricingQuantity:    &usageQuantity,
			PricingUnit:        optionalString(record.UsageUnit),
			PricingCategory:    optionalString(record.Cloud),
		}

		customCost := pb.CustomCost{
			BilledCost:         billedCost,
			ListCost:           billedCost,
			AccountName:        firstNonEmpty(record.AccountID, config.AccountID),
			ChargeCategory:     chargeCategory,
			Description:        databricksDescription(record),
			ResourceName:       resourceName,
			ResourceType:       serviceName,
			Id:                 uuid.New().String(),
			ProviderId:         providerID,
			UsageQuantity:      usageQuantity,
			UsageUnit:          record.UsageUnit,
			ExtendedAttributes: &extendedAttrs,
		}

		customCosts = append(customCosts, &customCost)
	}

	return customCosts
}

func unitPriceForSKU(config *databricksplugin.DatabricksConfig, sku string) float64 {
	if config == nil {
		return 0
	}
	if price, ok := config.SKUUnitPrices[sku]; ok {
		return price
	}
	return config.DefaultUnitPrice
}

func databricksDescription(record usageRecord) string {
	parts := compact([]string{"Databricks", record.BillingOriginProduct, record.UsageType, record.SKUName})
	if len(parts) == 0 {
		return "Databricks usage"
	}
	return strings.Join(parts, " ")
}

func parseDatabricksTimeOrFallback(value string, fallback time.Time) time.Time {
	parsed, err := parseDatabricksTime(value)
	if err != nil {
		return fallback.UTC()
	}
	return parsed.UTC()
}

func parseDatabricksTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}

	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05.999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05.999",
		"2006-01-02 15:04:05",
		databricksDateFormat,
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported timestamp %q", value)
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

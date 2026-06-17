package newrelicplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	DefaultNerdGraphURL      = "https://api.newrelic.com/graphql"
	DefaultCurrency          = "USD"
	DefaultQuantityAttribute = "usageQuantity"
	MetricDataIngest         = "gigabytes_ingested"
	MetricCoreCCU            = "core_ccu"
	MetricAdvancedCCU        = "advanced_ccu"
	MetricSyntheticChecks    = "synthetic_checks"
)

type NewRelicConfig struct {
	APIKey                     string             `json:"new_relic_api_key"`
	AccountID                  int                `json:"account_id"`
	NerdGraphURL               string             `json:"nerdgraph_url"`
	AccountName                string             `json:"account_name"`
	Currency                   string             `json:"currency"`
	GigabytesIngestedUnitPrice float64            `json:"gigabytes_ingested_unit_price"`
	CoreCCUUnitPrice           float64            `json:"core_ccu_unit_price"`
	AdvancedCCUUnitPrice       float64            `json:"advanced_ccu_unit_price"`
	SyntheticCheckUnitPrice    float64            `json:"synthetic_check_unit_price"`
	MetricPrices               map[string]float64 `json:"metric_prices"`
	UsageQueries               []UsageQueryConfig `json:"usage_queries"`
	LogLevel                   string             `json:"log_level"`
}

type UsageQueryConfig struct {
	Name              string   `json:"name"`
	NRQL              string   `json:"nrql"`
	QuantityAttribute string   `json:"quantity_attribute"`
	Unit              string   `json:"unit"`
	UnitPrice         float64  `json:"unit_price"`
	PriceKey          string   `json:"price_key"`
	ResourceType      string   `json:"resource_type"`
	FacetAttributes   []string `json:"facet_attributes"`
}

func GetNewRelicConfig(configFilePath string) (*NewRelicConfig, error) {
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading New Relic config file at %s: %v", configFilePath, err)
	}

	var result NewRelicConfig
	if err := json.Unmarshal(bytes, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling New Relic config: %v", err)
	}

	if strings.TrimSpace(result.APIKey) == "" {
		return nil, fmt.Errorf("new_relic_api_key is required")
	}
	if result.AccountID <= 0 {
		return nil, fmt.Errorf("account_id is required")
	}
	if strings.TrimSpace(result.NerdGraphURL) == "" {
		result.NerdGraphURL = DefaultNerdGraphURL
	}
	if strings.TrimSpace(result.AccountName) == "" {
		result.AccountName = fmt.Sprintf("new-relic-account-%d", result.AccountID)
	}
	if strings.TrimSpace(result.Currency) == "" {
		result.Currency = DefaultCurrency
	}
	if strings.TrimSpace(result.LogLevel) == "" {
		result.LogLevel = "info"
	}

	if result.GigabytesIngestedUnitPrice < 0 {
		return nil, fmt.Errorf("gigabytes_ingested_unit_price must not be negative")
	}
	if result.CoreCCUUnitPrice < 0 {
		return nil, fmt.Errorf("core_ccu_unit_price must not be negative")
	}
	if result.AdvancedCCUUnitPrice < 0 {
		return nil, fmt.Errorf("advanced_ccu_unit_price must not be negative")
	}
	if result.SyntheticCheckUnitPrice < 0 {
		return nil, fmt.Errorf("synthetic_check_unit_price must not be negative")
	}
	for metric, price := range result.MetricPrices {
		if strings.TrimSpace(metric) == "" {
			return nil, fmt.Errorf("metric_prices cannot contain an empty metric")
		}
		if price < 0 {
			return nil, fmt.Errorf("metric_prices[%q] must not be negative", metric)
		}
	}

	for i := range result.UsageQueries {
		query := &result.UsageQueries[i]
		if strings.TrimSpace(query.Name) == "" {
			return nil, fmt.Errorf("usage_queries[%d].name is required", i)
		}
		if strings.TrimSpace(query.NRQL) == "" {
			return nil, fmt.Errorf("usage_queries[%d].nrql is required", i)
		}
		if strings.TrimSpace(query.QuantityAttribute) == "" {
			query.QuantityAttribute = DefaultQuantityAttribute
		}
		if strings.TrimSpace(query.Unit) == "" {
			query.Unit = "unit"
		}
		if query.UnitPrice < 0 {
			return nil, fmt.Errorf("usage_queries[%d].unit_price must not be negative", i)
		}
		if strings.TrimSpace(query.PriceKey) == "" {
			query.PriceKey = query.Name
		}
		if strings.TrimSpace(query.ResourceType) == "" {
			query.ResourceType = query.Name
		}
	}

	return &result, nil
}

func DefaultUsageQueries() []UsageQueryConfig {
	return []UsageQueryConfig{
		{
			Name:              MetricDataIngest,
			NRQL:              "FROM NrConsumption SELECT sum(GigabytesIngested) AS usageQuantity WHERE productLine = 'DataPlatform' SINCE '{start}' UNTIL '{end}' FACET usageMetric, consumingAccountId",
			QuantityAttribute: DefaultQuantityAttribute,
			Unit:              "GB",
			PriceKey:          MetricDataIngest,
			ResourceType:      "DataPlatform",
			FacetAttributes:   []string{"usageMetric", "consumingAccountId"},
		},
		{
			Name:              MetricCoreCCU,
			NRQL:              "FROM NrConsumption SELECT sum(consumption) AS usageQuantity WHERE metric = 'CoreCCU' SINCE '{start}' UNTIL '{end}' FACET dimension_productCapability, consumingAccountId",
			QuantityAttribute: DefaultQuantityAttribute,
			Unit:              "CCU",
			PriceKey:          MetricCoreCCU,
			ResourceType:      "CoreCCU",
			FacetAttributes:   []string{"dimension_productCapability", "consumingAccountId"},
		},
		{
			Name:              MetricAdvancedCCU,
			NRQL:              "FROM NrConsumption SELECT sum(consumption) AS usageQuantity WHERE metric = 'AdvancedCCU' SINCE '{start}' UNTIL '{end}' FACET dimension_productCapability, consumingAccountId",
			QuantityAttribute: DefaultQuantityAttribute,
			Unit:              "CCU",
			PriceKey:          MetricAdvancedCCU,
			ResourceType:      "AdvancedCCU",
			FacetAttributes:   []string{"dimension_productCapability", "consumingAccountId"},
		},
		{
			Name:              MetricSyntheticChecks,
			NRQL:              "FROM NrDailyUsage SELECT (sum(syntheticsFailedCheckCount) + sum(syntheticsSuccessCheckCount)) AS usageQuantity WHERE syntheticsTypeLabel != 'Ping' SINCE '{start}' UNTIL '{end}' FACET syntheticsTypeLabel, syntheticsMonitorName, consumingAccountId",
			QuantityAttribute: DefaultQuantityAttribute,
			Unit:              "check",
			PriceKey:          MetricSyntheticChecks,
			ResourceType:      "SyntheticChecks",
			FacetAttributes:   []string{"syntheticsTypeLabel", "syntheticsMonitorName", "consumingAccountId"},
		},
	}
}

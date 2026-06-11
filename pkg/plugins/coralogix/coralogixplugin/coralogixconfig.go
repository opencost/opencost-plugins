package coralogixplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	DefaultAPIBaseURL  = "https://api.coralogix.com/mgmt/openapi/latest"
	DefaultCurrency    = "USD"
	DateRangeStyleForm = "form"
	DateRangeStyleDeep = "deep_object"
	DateRangeStyleDot  = "dotted"
	DateRangeStyleJSON = "json"
)

type CoralogixConfig struct {
	APIKey          string             `json:"coralogix_api_key"`
	APIBaseURL      string             `json:"api_base_url"`
	DateRangeStyle  string             `json:"date_range_style"`
	Resolution      string             `json:"resolution"`
	AggregateBy     []string           `json:"aggregate_by"`
	SizeGBUnitPrice float64            `json:"size_gb_unit_price"`
	UnitPrice       float64            `json:"unit_price"`
	MetricPrices    map[string]float64 `json:"metric_prices"`
	Currency        string             `json:"currency"`
	LogLevel        string             `json:"log_level"`
}

func GetCoralogixConfig(configFilePath string) (*CoralogixConfig, error) {
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading CoraLogix config file at %s: %v", configFilePath, err)
	}

	var result CoralogixConfig
	if err := json.Unmarshal(bytes, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling CoraLogix config: %v", err)
	}

	if strings.TrimSpace(result.APIKey) == "" {
		return nil, fmt.Errorf("coralogix_api_key is required")
	}
	if strings.TrimSpace(result.APIBaseURL) == "" {
		result.APIBaseURL = DefaultAPIBaseURL
	}
	if strings.TrimSpace(result.DateRangeStyle) == "" {
		result.DateRangeStyle = DateRangeStyleForm
	}
	if !validDateRangeStyle(result.DateRangeStyle) {
		return nil, fmt.Errorf("date_range_style must be one of %s, %s, %s, %s", DateRangeStyleForm, DateRangeStyleDeep, DateRangeStyleDot, DateRangeStyleJSON)
	}
	if result.SizeGBUnitPrice < 0 {
		return nil, fmt.Errorf("size_gb_unit_price must not be negative")
	}
	if result.UnitPrice < 0 {
		return nil, fmt.Errorf("unit_price must not be negative")
	}
	for metric, price := range result.MetricPrices {
		if strings.TrimSpace(metric) == "" {
			return nil, fmt.Errorf("metric_prices cannot contain an empty metric")
		}
		if price < 0 {
			return nil, fmt.Errorf("metric_prices[%q] must not be negative", metric)
		}
	}
	for _, aggregate := range result.AggregateBy {
		if strings.TrimSpace(aggregate) == "" {
			return nil, fmt.Errorf("aggregate_by cannot contain an empty aggregate")
		}
	}
	if strings.TrimSpace(result.Currency) == "" {
		result.Currency = DefaultCurrency
	}
	if strings.TrimSpace(result.LogLevel) == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

func validDateRangeStyle(value string) bool {
	switch value {
	case DateRangeStyleForm, DateRangeStyleDeep, DateRangeStyleDot, DateRangeStyleJSON:
		return true
	default:
		return false
	}
}

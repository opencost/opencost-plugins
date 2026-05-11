package databricksplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const DefaultAccountAPIBaseURL = "https://accounts.cloud.databricks.com"

type DatabricksConfig struct {
	AccountID           string             `json:"account_id"`
	Token               string             `json:"token"`
	AccountAPIBaseURL   string             `json:"account_api_base_url"`
	IncludePersonalData bool               `json:"include_personal_data"`
	SKUUnitPrices       map[string]float64 `json:"sku_unit_prices"`
	DefaultUnitPrice    float64            `json:"default_unit_price"`
	LogLevel            string             `json:"log_level"`
}

func GetDatabricksConfig(configFilePath string) (*DatabricksConfig, error) {
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading Databricks config file at %s: %v", configFilePath, err)
	}

	var result DatabricksConfig
	if err := json.Unmarshal(bytes, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling Databricks config: %v", err)
	}

	if strings.TrimSpace(result.AccountID) == "" {
		return nil, fmt.Errorf("account_id is required")
	}
	if strings.TrimSpace(result.Token) == "" {
		return nil, fmt.Errorf("token is required")
	}
	if strings.TrimSpace(result.AccountAPIBaseURL) == "" {
		result.AccountAPIBaseURL = DefaultAccountAPIBaseURL
	}
	if result.DefaultUnitPrice < 0 {
		return nil, fmt.Errorf("default_unit_price must not be negative")
	}
	for sku, price := range result.SKUUnitPrices {
		if strings.TrimSpace(sku) == "" {
			return nil, fmt.Errorf("sku_unit_prices cannot contain an empty SKU")
		}
		if price < 0 {
			return nil, fmt.Errorf("sku_unit_prices[%q] must not be negative", sku)
		}
	}
	if strings.TrimSpace(result.LogLevel) == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

package cloudamqpplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const DefaultAPIBaseURL = "https://customer.cloudamqp.com/api"

type CloudAMQPConfig struct {
	APIKey      string `json:"api_key"`
	APIBaseURL  string `json:"api_base_url"`
	AccountName string `json:"account_name"`
	Currency    string `json:"currency"`
	LogLevel    string `json:"log_level"`
}

func GetCloudAMQPConfig(configFilePath string) (*CloudAMQPConfig, error) {
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading CloudAMQP config file at %s: %v", configFilePath, err)
	}

	var result CloudAMQPConfig
	if err := json.Unmarshal(bytes, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling CloudAMQP config: %v", err)
	}

	if strings.TrimSpace(result.APIKey) == "" {
		return nil, fmt.Errorf("api_key is required")
	}
	if strings.TrimSpace(result.APIBaseURL) == "" {
		result.APIBaseURL = DefaultAPIBaseURL
	}
	if strings.TrimSpace(result.AccountName) == "" {
		result.AccountName = "cloudamqp"
	}
	if strings.TrimSpace(result.Currency) == "" {
		result.Currency = "USD"
	}
	if strings.TrimSpace(result.LogLevel) == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

package confluentplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	DefaultAPIBaseURL = "https://api.confluent.cloud"
	DefaultPageSize   = 5000
	MaxPageSize       = 10000
)

type ConfluentConfig struct {
	APIKey     string `json:"confluent_api_key"`
	APISecret  string `json:"confluent_api_secret"`
	APIBaseURL string `json:"api_base_url"`
	PageSize   int    `json:"page_size"`
	LogLevel   string `json:"log_level"`
}

func GetConfluentConfig(configFilePath string) (*ConfluentConfig, error) {
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading Confluent config file at %s: %v", configFilePath, err)
	}

	var result ConfluentConfig
	if err := json.Unmarshal(bytes, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling Confluent config: %v", err)
	}

	if strings.TrimSpace(result.APIKey) == "" {
		return nil, fmt.Errorf("confluent_api_key is required")
	}
	if strings.TrimSpace(result.APISecret) == "" {
		return nil, fmt.Errorf("confluent_api_secret is required")
	}
	if strings.TrimSpace(result.APIBaseURL) == "" {
		result.APIBaseURL = DefaultAPIBaseURL
	}
	if result.PageSize == 0 {
		result.PageSize = DefaultPageSize
	}
	if result.PageSize < 0 || result.PageSize > MaxPageSize {
		return nil, fmt.Errorf("page_size must be between 1 and %d", MaxPageSize)
	}
	if strings.TrimSpace(result.LogLevel) == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

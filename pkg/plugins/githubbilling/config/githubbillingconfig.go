package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type GitHubBillingConfig struct {
	APIToken       string `json:"github_api_token"`
	Organization   string `json:"github_organization"`
	LogLevel       string `json:"github_billing_plugin_log_level"`
}

func GetGitHubBillingConfig(configFilePath string) (*GitHubBillingConfig, error) {
	var result GitHubBillingConfig
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file for GitHub Billing config @ %s: %v", configFilePath, err)
	}
	err = json.Unmarshal(bytes, &result)
	if err != nil {
		return nil, fmt.Errorf("error marshaling json into GitHub Billing config %v", err)
	}

	if result.LogLevel == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

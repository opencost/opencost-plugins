package cloudflareplugin

import (
	"encoding/json"
	"fmt"
	"os"
)

// CloudflareConfig holds the configuration for the Cloudflare plugin
type CloudflareConfig struct {
	APIKey    string `json:"cloudflare_api_key"`
	Email     string `json:"cloudflare_email"`
	AccountID string `json:"cloudflare_account_id"`
	LogLevel  string `json:"cloudflare_plugin_log_level"`
}

// GetCloudflareConfig reads and parses the Cloudflare configuration file
func GetCloudflareConfig(configFilePath string) (*CloudflareConfig, error) {
	var result CloudflareConfig
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file for Cloudflare config @ %s: %v", configFilePath, err)
	}
	err = json.Unmarshal(bytes, &result)
	if err != nil {
		return nil, fmt.Errorf("error marshaling json into Cloudflare config %v", err)
	}

	if result.LogLevel == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

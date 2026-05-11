package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type FastlyConfig struct {
	APIKey   string `json:"fastly_api_key"`
	LogLevel string `json:"fastly_plugin_log_level"`
}

func GetFastlyConfig(configFilePath string) (*FastlyConfig, error) {
	var result FastlyConfig
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file for Fastly config @ %s: %v", configFilePath, err)
	}
	err = json.Unmarshal(bytes, &result)
	if err != nil {
		return nil, fmt.Errorf("error marshaling json into Fastly config %v", err)
	}

	if result.LogLevel == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

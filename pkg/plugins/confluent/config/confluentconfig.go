package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type ConfluentConfig struct {
	APIKey    string `json:"confluent_api_key"`
	APISecret string `json:"confluent_api_secret"`
	LogLevel  string `json:"confluent_plugin_log_level"`
}

func GetConfluentConfig(configFilePath string) (*ConfluentConfig, error) {
	var result ConfluentConfig
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file for Confluent config @ %s: %v", configFilePath, err)
	}
	err = json.Unmarshal(bytes, &result)
	if err != nil {
		return nil, fmt.Errorf("error marshaling json into Confluent config %v", err)
	}

	if result.LogLevel == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

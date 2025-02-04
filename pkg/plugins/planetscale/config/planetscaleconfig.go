package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type PlanetScaleConfig struct {
	APIKey     string `json:"planetscale_api_key"`
	OrgID      string `json:"planetscale_org_id"`
	Database   string `json:"planetscale_database"`
	LogLevel   string `json:"planetscale_plugin_log_level"`
}

// GetPlanetscaleConfig reads the configuration from a given JSON file.
func GetPlanetscaleConfig(configFilePath string) (*PlanetScaleConfig, error) {
	var result PlanetScaleConfig
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file for PlanetScale config @ %s: %v", configFilePath, err)
	}
	err = json.Unmarshal(bytes, &result)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling JSON into PlanetScale config: %v", err)
	}

	// Set default log level if not provided
	if result.LogLevel == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type AtlasConfig struct {
	PublicKey  string `json:"atlas_public_key"`
	PrivateKey string `json:"atlas_private_key"`
	OrgID      string `json:"atlas_org_id"`
	LogLevel   string `json:"atlas_plugin_log_level"`
}

func GetAtlasConfig(configFilePath string) (*AtlasConfig, error) {
	var result AtlasConfig
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file for Atlas config @ %s: %v", configFilePath, err)
	}
	err = json.Unmarshal(bytes, &result)
	if err != nil {
		return nil, fmt.Errorf("error marshaling json into Atlas config %v", err)
	}

	if result.LogLevel == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

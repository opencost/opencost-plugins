package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type SnowflakeConfig struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	Account   string `json:"account"`
	Database  string `json:"database"`
	Schema    string `json:"schema"`
	Warehouse string `json:"warehouse"`
	LogLevel  string `json:"snowflake_plugin_log_level"`
}

func GetSnowflakeConfig(configFilePath string) (*SnowflakeConfig, error) {
	var result SnowflakeConfig
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file for Snowflake config @ %s: %v", configFilePath, err)
	}
	err = json.Unmarshal(bytes, &result)
	if err != nil {
		return nil, fmt.Errorf("error marshaling json into Snowflake config %v", err)
	}
	return &result, nil
}

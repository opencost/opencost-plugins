package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type SnowflakeConfig struct {
	Account   string `json:"snowflake_account"`
	Username  string `json:"snowflake_username"`
	Password  string `json:"snowflake_password"`
	Database  string `json:"snowflake_database"`
	Warehouse string `json:"snowflake_warehouse"`
	Role      string `json:"snowflake_role"`
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

	if result.LogLevel == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

package cloudamqpplugin

import (
	"encoding/json"
	"fmt"
	"os"
)

type CloudAMQPConfig struct {
	APIKey    string `json:"cloudamqp_api_key"`
	AccountID string `json:"cloudamqp_account_id"`
	LogLevel  string `json:"cloudamqp_plugin_log_level"`
}

func GetCloudAMQPConfig(configFilePath string) (*CloudAMQPConfig, error) {
	var result CloudAMQPConfig
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file for CloudAMQP config @ %s: %v", configFilePath, err)
	}
	err = json.Unmarshal(bytes, &result)
	if err != nil {
		return nil, fmt.Errorf("error marshaling json into CloudAMQP config %v", err)
	}

	if result.LogLevel == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

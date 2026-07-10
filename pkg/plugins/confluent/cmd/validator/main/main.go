package main

import (
	"fmt"
	"os"

	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	confluentconfig "github.com/opencost/opencost-plugins/pkg/plugins/confluent/config"
	"github.com/opencost/opencost/core/pkg/log"
)

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	confluentCfg, err := confluentconfig.GetConfluentConfig(configFile)
	if err != nil {
		log.Fatalf("error building Confluent config: %v", err)
	}

	// Validate required fields
	if confluentCfg.APIKey == "" {
		log.Fatalf("confluent_api_key is required in config file")
	}
	if confluentCfg.APISecret == "" {
		log.Fatalf("confluent_api_secret is required in config file")
	}

	fmt.Printf("Confluent config validated successfully\n")
	fmt.Printf("Log level: %s\n", confluentCfg.LogLevel)
	fmt.Printf("\nSample config:\n")
	fmt.Printf(`{
  "confluent_api_key": "YOUR_API_KEY",
  "confluent_api_secret": "YOUR_API_SECRET",
  "confluent_plugin_log_level": "info"
}
`)
	os.Exit(0)
}

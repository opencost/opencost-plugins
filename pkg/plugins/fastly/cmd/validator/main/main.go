package main

import (
	"encoding/json"
	"fmt"
	"os"

	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	fastlyconfig "github.com/opencost/opencost-plugins/pkg/plugins/fastly/config"
	"github.com/opencost/opencost/core/pkg/log"
)

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	fastlyCfg, err := fastlyconfig.GetFastlyConfig(configFile)
	if err != nil {
		log.Fatalf("error building Fastly config: %v", err)
	}

	// Validate required fields
	if fastlyCfg.APIKey == "" {
		log.Fatalf("fastly_api_key is required in config file")
	}

	fmt.Printf("Fastly config validated successfully\n")
	fmt.Printf("Log level: %s\n", fastlyCfg.LogLevel)

	// Write out a sample config for reference
	sampleConfig := map[string]interface{}{
		"fastly_api_key":          "YOUR_FASTLY_API_KEY",
		"fastly_plugin_log_level": "info",
	}
	sampleJSON, _ := json.MarshalIndent(sampleConfig, "", "  ")
	fmt.Printf("\nSample config:\n%s\n", string(sampleJSON))

	os.Exit(0)
}

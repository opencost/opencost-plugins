package main

import (
	"fmt"
	"os"

	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	cloudamqpconfig "github.com/opencost/opencost-plugins/pkg/plugins/cloudamqp/config"
	"github.com/opencost/opencost/core/pkg/log"
)

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	cloudamqpCfg, err := cloudamqpconfig.GetCloudAMQPConfig(configFile)
	if err != nil {
		log.Fatalf("error building CloudAMQP config: %v", err)
	}

	if cloudamqpCfg.APIKey == "" {
		log.Fatalf("cloudamqp_api_key is required in config file")
	}

	fmt.Printf("CloudAMQP config validated successfully\n")
	fmt.Printf("Log level: %s\n", cloudamqpCfg.LogLevel)
	fmt.Printf("\nSample config:\n")
	fmt.Printf(`{
  "cloudamqp_api_key": "YOUR_API_KEY",
  "cloudamqp_plugin_log_level": "info"
}
`)
	os.Exit(0)
}

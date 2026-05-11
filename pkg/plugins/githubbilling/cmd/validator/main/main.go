package main

import (
	"fmt"
	"os"

	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	githubbillingconfig "github.com/opencost/opencost-plugins/pkg/plugins/githubbilling/config"
	"github.com/opencost/opencost/core/pkg/log"
)

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	githubCfg, err := githubbillingconfig.GetGitHubBillingConfig(configFile)
	if err != nil {
		log.Fatalf("error building GitHub Billing config: %v", err)
	}

	if githubCfg.APIToken == "" {
		log.Fatalf("github_api_token is required in config file")
	}
	if githubCfg.Organization == "" {
		log.Fatalf("github_organization is required in config file")
	}

	fmt.Printf("GitHub Billing config validated successfully\n")
	fmt.Printf("Organization: %s\n", githubCfg.Organization)
	fmt.Printf("Log level: %s\n", githubCfg.LogLevel)
	fmt.Printf("\nSample config:\n")
	fmt.Printf(`{
  "github_api_token": "ghp_YOUR_TOKEN",
  "github_organization": "your-org-name",
  "github_billing_plugin_log_level": "info"
}
`)
	os.Exit(0)
}

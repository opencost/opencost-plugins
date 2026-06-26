package githubplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	AccountTypeOrg  = "org"
	AccountTypeUser = "user"
)

type GitHubConfig struct {
	Token       string `json:"github_token"`
	Account     string `json:"account"`
	AccountType string `json:"account_type"`
	APIBaseURL  string `json:"api_base_url"`
	APIVersion  string `json:"api_version"`
	LogLevel    string `json:"log_level"`
}

func GetGitHubConfig(configFilePath string) (*GitHubConfig, error) {
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading GitHub config file at %s: %v", configFilePath, err)
	}

	var result GitHubConfig
	if err := json.Unmarshal(bytes, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling GitHub config: %v", err)
	}

	result.AccountType = strings.ToLower(strings.TrimSpace(result.AccountType))
	if result.AccountType == "" {
		result.AccountType = AccountTypeOrg
	}
	if result.AccountType != AccountTypeOrg && result.AccountType != AccountTypeUser {
		return nil, fmt.Errorf("account_type must be %q or %q", AccountTypeOrg, AccountTypeUser)
	}
	if strings.TrimSpace(result.Account) == "" {
		return nil, fmt.Errorf("account is required")
	}
	if strings.TrimSpace(result.Token) == "" {
		return nil, fmt.Errorf("github_token is required")
	}
	if strings.TrimSpace(result.APIBaseURL) == "" {
		result.APIBaseURL = "https://api.github.com"
	}
	if strings.TrimSpace(result.APIVersion) == "" {
		result.APIVersion = "2022-11-28"
	}
	if strings.TrimSpace(result.LogLevel) == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

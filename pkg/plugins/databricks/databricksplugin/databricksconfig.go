package databricksplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	DefaultAccountAPIBaseURL = "https://accounts.cloud.databricks.com"
	DefaultScope             = "all-apis"
)

// Authentication methods, chosen from which config fields are set.
const (
	AuthMethodToken        = "token"
	AuthMethodClientSecret = "client_secret"
	AuthMethodFederated    = "federated"
)

type DatabricksConfig struct {
	AccountID string `json:"account_id"`
	// Token is a pre-issued access token. Databricks account-level OAuth
	// tokens expire after one hour, so this is only suitable for testing.
	Token string `json:"token"`
	// ClientID is the service principal's application (client) ID. Required
	// with client_secret; optional with federated_token_file (omit it for
	// account-wide federation policies).
	ClientID string `json:"client_id"`
	// ClientSecret is a service principal OAuth secret (OAuth M2M).
	ClientSecret string `json:"client_secret"`
	// FederatedTokenFile is a path to a JWT from an identity provider trusted
	// by a Databricks federation policy, such as a projected Kubernetes
	// service account token. The file is re-read on every token refresh.
	FederatedTokenFile string `json:"federated_token_file"`
	// Scope requested for OAuth tokens. Defaults to "all-apis". Use the scope
	// assigned to the secret, for example "billing:read".
	Scope               string             `json:"scope"`
	AccountAPIBaseURL   string             `json:"account_api_base_url"`
	IncludePersonalData bool               `json:"include_personal_data"`
	SKUUnitPrices       map[string]float64 `json:"sku_unit_prices"`
	DefaultUnitPrice    float64            `json:"default_unit_price"`
	LogLevel            string             `json:"log_level"`
}

func GetDatabricksConfig(configFilePath string) (*DatabricksConfig, error) {
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading Databricks config file at %s: %v", configFilePath, err)
	}

	var result DatabricksConfig
	if err := json.Unmarshal(bytes, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling Databricks config: %v", err)
	}

	if strings.TrimSpace(result.AccountID) == "" {
		return nil, fmt.Errorf("account_id is required")
	}
	if _, err := result.AuthMethod(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(result.Scope) == "" {
		result.Scope = DefaultScope
	}
	if strings.TrimSpace(result.AccountAPIBaseURL) == "" {
		result.AccountAPIBaseURL = DefaultAccountAPIBaseURL
	}
	if result.DefaultUnitPrice < 0 {
		return nil, fmt.Errorf("default_unit_price must not be negative")
	}
	for sku, price := range result.SKUUnitPrices {
		if strings.TrimSpace(sku) == "" {
			return nil, fmt.Errorf("sku_unit_prices cannot contain an empty SKU")
		}
		if price < 0 {
			return nil, fmt.Errorf("sku_unit_prices[%q] must not be negative", sku)
		}
	}
	if strings.TrimSpace(result.LogLevel) == "" {
		result.LogLevel = "info"
	}

	return &result, nil
}

// AuthMethod returns which authentication method the config selects. Exactly
// one of token, client_secret or federated_token_file must be set.
func (c *DatabricksConfig) AuthMethod() (string, error) {
	hasToken := strings.TrimSpace(c.Token) != ""
	hasSecret := strings.TrimSpace(c.ClientSecret) != ""
	hasFederated := strings.TrimSpace(c.FederatedTokenFile) != ""

	set := 0
	for _, b := range []bool{hasToken, hasSecret, hasFederated} {
		if b {
			set++
		}
	}
	if set == 0 {
		return "", fmt.Errorf("one of client_secret (with client_id), federated_token_file or token is required")
	}
	if set > 1 {
		return "", fmt.Errorf("only one of token, client_secret and federated_token_file may be set")
	}

	switch {
	case hasSecret:
		if strings.TrimSpace(c.ClientID) == "" {
			return "", fmt.Errorf("client_id is required with client_secret")
		}
		return AuthMethodClientSecret, nil
	case hasFederated:
		return AuthMethodFederated, nil
	default:
		return AuthMethodToken, nil
	}
}

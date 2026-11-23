package databricksplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetDatabricksConfigDefaults(t *testing.T) {
	path := writeConfig(t, `{
		"account_id": "abc-123",
		"token": "test-token"
	}`)

	config, err := GetDatabricksConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.AccountAPIBaseURL != DefaultAccountAPIBaseURL {
		t.Fatalf("unexpected account API base URL: %s", config.AccountAPIBaseURL)
	}
	if config.LogLevel != "info" {
		t.Fatalf("unexpected log level: %s", config.LogLevel)
	}
}

func TestGetDatabricksConfigValidatesRequiredFields(t *testing.T) {
	path := writeConfig(t, `{"account_id": "abc-123"}`)

	_, err := GetDatabricksConfig(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestGetDatabricksConfigValidatesPrices(t *testing.T) {
	path := writeConfig(t, `{
		"account_id": "abc-123",
		"token": "test-token",
		"sku_unit_prices": {
			"SKU_A": -1
		}
	}`)

	_, err := GetDatabricksConfig(path)
	if err == nil {
		t.Fatalf("expected price validation error")
	}
}

func TestGetDatabricksConfigAuthMethods(t *testing.T) {
	cases := []struct {
		name       string
		json       string
		wantMethod string
		wantErr    string
	}{
		{"static token", `{"account_id":"a","token":"t"}`, AuthMethodToken, ""},
		{"client secret", `{"account_id":"a","client_id":"c","client_secret":"s"}`, AuthMethodClientSecret, ""},
		{"federated with client id", `{"account_id":"a","client_id":"c","federated_token_file":"/var/run/token"}`, AuthMethodFederated, ""},
		{"federated account-wide", `{"account_id":"a","federated_token_file":"/var/run/token"}`, AuthMethodFederated, ""},
		{"nothing", `{"account_id":"a"}`, "", "is required"},
		{"secret without client id", `{"account_id":"a","client_secret":"s"}`, "", "client_id is required"},
		{"token and secret", `{"account_id":"a","token":"t","client_id":"c","client_secret":"s"}`, "", "only one of"},
		{"secret and federated", `{"account_id":"a","client_id":"c","client_secret":"s","federated_token_file":"/f"}`, "", "only one of"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config, err := GetDatabricksConfig(writeConfig(t, tc.json))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if method, _ := config.AuthMethod(); method != tc.wantMethod {
				t.Fatalf("auth method %q, want %q", method, tc.wantMethod)
			}
		})
	}
}

func TestGetDatabricksConfigScope(t *testing.T) {
	config, err := GetDatabricksConfig(writeConfig(t, `{"account_id":"a","client_id":"c","client_secret":"s"}`))
	if err != nil {
		t.Fatal(err)
	}
	if config.Scope != DefaultScope {
		t.Fatalf("default scope %q, want %q", config.Scope, DefaultScope)
	}

	config, err = GetDatabricksConfig(writeConfig(t, `{"account_id":"a","client_id":"c","client_secret":"s","scope":"billing:read"}`))
	if err != nil {
		t.Fatal(err)
	}
	if config.Scope != "billing:read" {
		t.Fatalf("scope %q, want billing:read", config.Scope)
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

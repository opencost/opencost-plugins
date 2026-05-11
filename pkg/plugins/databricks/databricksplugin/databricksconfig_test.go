package databricksplugin

import (
	"os"
	"path/filepath"
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

func writeConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

package coralogixplugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetCoralogixConfigDefaults(t *testing.T) {
	path := writeConfig(t, `{
		"coralogix_api_key": "test-key"
	}`)

	config, err := GetCoralogixConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.APIBaseURL != DefaultAPIBaseURL {
		t.Fatalf("unexpected API base URL: %s", config.APIBaseURL)
	}
	if config.DateRangeStyle != DateRangeStyleForm {
		t.Fatalf("unexpected date range style: %s", config.DateRangeStyle)
	}
	if config.Currency != DefaultCurrency {
		t.Fatalf("unexpected currency: %s", config.Currency)
	}
	if config.LogLevel != "info" {
		t.Fatalf("unexpected log level: %s", config.LogLevel)
	}
}

func TestGetCoralogixConfigValidatesRequiredFields(t *testing.T) {
	path := writeConfig(t, `{}`)

	_, err := GetCoralogixConfig(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestGetCoralogixConfigValidatesPrices(t *testing.T) {
	path := writeConfig(t, `{
		"coralogix_api_key": "test-key",
		"metric_prices": {
			"gb_sent": -1
		}
	}`)

	_, err := GetCoralogixConfig(path)
	if err == nil {
		t.Fatalf("expected price validation error")
	}
}

func TestGetCoralogixConfigValidatesDateRangeStyle(t *testing.T) {
	path := writeConfig(t, `{
		"coralogix_api_key": "test-key",
		"date_range_style": "unknown"
	}`)

	_, err := GetCoralogixConfig(path)
	if err == nil {
		t.Fatalf("expected date range style validation error")
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

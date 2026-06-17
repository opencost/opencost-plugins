package newrelicplugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetNewRelicConfigDefaults(t *testing.T) {
	configPath := writeConfig(t, `{
		"new_relic_api_key": "test-key",
		"account_id": 12345
	}`)

	config, err := GetNewRelicConfig(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.NerdGraphURL != DefaultNerdGraphURL {
		t.Fatalf("unexpected NerdGraph URL: %s", config.NerdGraphURL)
	}
	if config.AccountName != "new-relic-account-12345" {
		t.Fatalf("unexpected account name: %s", config.AccountName)
	}
	if config.Currency != DefaultCurrency {
		t.Fatalf("unexpected currency: %s", config.Currency)
	}
	if config.LogLevel != "info" {
		t.Fatalf("unexpected log level: %s", config.LogLevel)
	}
}

func TestGetNewRelicConfigValidatesRequiredFields(t *testing.T) {
	configPath := writeConfig(t, `{"account_id": 12345}`)
	if _, err := GetNewRelicConfig(configPath); err == nil {
		t.Fatal("expected missing API key error")
	}

	configPath = writeConfig(t, `{"new_relic_api_key": "test-key"}`)
	if _, err := GetNewRelicConfig(configPath); err == nil {
		t.Fatal("expected missing account id error")
	}
}

func TestGetNewRelicConfigNormalizesUsageQueries(t *testing.T) {
	configPath := writeConfig(t, `{
		"new_relic_api_key": "test-key",
		"account_id": 12345,
		"usage_queries": [
			{"name": "custom", "nrql": "FROM NrConsumption SELECT sum(consumption) AS usageQuantity"}
		]
	}`)

	config, err := GetNewRelicConfig(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(config.UsageQueries) != 1 {
		t.Fatalf("expected one usage query, got %d", len(config.UsageQueries))
	}
	query := config.UsageQueries[0]
	if query.QuantityAttribute != DefaultQuantityAttribute {
		t.Fatalf("unexpected quantity attribute: %s", query.QuantityAttribute)
	}
	if query.Unit != "unit" {
		t.Fatalf("unexpected unit: %s", query.Unit)
	}
	if query.PriceKey != "custom" {
		t.Fatalf("unexpected price key: %s", query.PriceKey)
	}
	if query.ResourceType != "custom" {
		t.Fatalf("unexpected resource type: %s", query.ResourceType)
	}
}

func TestDefaultUsageQueries(t *testing.T) {
	queries := DefaultUsageQueries()
	if len(queries) != 4 {
		t.Fatalf("expected four default queries, got %d", len(queries))
	}
	for _, query := range queries {
		if query.Name == "" {
			t.Fatal("default query missing name")
		}
		if query.QuantityAttribute != DefaultQuantityAttribute {
			t.Fatalf("default query %s has unexpected quantity attribute: %s", query.Name, query.QuantityAttribute)
		}
		if query.NRQL == "" {
			t.Fatalf("default query %s missing NRQL", query.Name)
		}
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("error writing config: %v", err)
	}
	return path
}

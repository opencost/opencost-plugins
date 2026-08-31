package confluentplugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetConfluentConfigDefaults(t *testing.T) {
	path := writeConfig(t, `{
		"confluent_api_key": "key",
		"confluent_api_secret": "secret"
	}`)

	config, err := GetConfluentConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.APIBaseURL != DefaultAPIBaseURL {
		t.Fatalf("unexpected api base url: %s", config.APIBaseURL)
	}
	if config.PageSize != DefaultPageSize {
		t.Fatalf("unexpected page size: %d", config.PageSize)
	}
	if config.LogLevel != "info" {
		t.Fatalf("unexpected log level: %s", config.LogLevel)
	}
}

func TestGetConfluentConfigValidatesRequiredFields(t *testing.T) {
	path := writeConfig(t, `{"confluent_api_key": "key"}`)

	_, err := GetConfluentConfig(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestGetConfluentConfigValidatesPageSize(t *testing.T) {
	path := writeConfig(t, `{
		"confluent_api_key": "key",
		"confluent_api_secret": "secret",
		"page_size": 10001
	}`)

	_, err := GetConfluentConfig(path)
	if err == nil {
		t.Fatalf("expected page size validation error")
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

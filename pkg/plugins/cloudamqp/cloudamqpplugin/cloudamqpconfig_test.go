package cloudamqpplugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetCloudAMQPConfigDefaults(t *testing.T) {
	path := writeConfig(t, `{"api_key": "test-key"}`)

	config, err := GetCloudAMQPConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.APIBaseURL != DefaultAPIBaseURL {
		t.Fatalf("unexpected api base url: %s", config.APIBaseURL)
	}
	if config.AccountName != "cloudamqp" {
		t.Fatalf("unexpected account name: %s", config.AccountName)
	}
	if config.Currency != "USD" {
		t.Fatalf("unexpected currency: %s", config.Currency)
	}
	if config.LogLevel != "info" {
		t.Fatalf("unexpected log level: %s", config.LogLevel)
	}
}

func TestGetCloudAMQPConfigValidatesRequiredFields(t *testing.T) {
	path := writeConfig(t, `{}`)

	_, err := GetCloudAMQPConfig(path)
	if err == nil {
		t.Fatalf("expected validation error")
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

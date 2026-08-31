package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetFastlyConfig(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	configData := `{
        "fastly_api_key": "test-api-key",
        "log_level": "debug"
    }`
	err := os.WriteFile(configPath, []byte(configData), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	// Act
	config, err := GetFastlyConfig(configPath)

	// Assert
	if err != nil {
		t.Fatalf("GetFastlyConfig returned error: %v", err)
	}
	if config == nil {
		t.Fatal("Expected config to be non-nil")
	}
	if config.FastlyAPIKey != "test-api-key" {
		t.Errorf("Expected API key 'test-api-key', got '%s'", config.FastlyAPIKey)
	}
	if config.LogLevel != "debug" {
		t.Errorf("Expected log level 'debug', got '%s'", config.LogLevel)
	}
}

func TestGetFastlyConfigDefaultLogLevel(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	configData := `{
        "fastly_api_key": "test-api-key"
    }`
	err := os.WriteFile(configPath, []byte(configData), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	// Act
	config, err := GetFastlyConfig(configPath)

	// Assert
	if err != nil {
		t.Fatalf("GetFastlyConfig returned error: %v", err)
	}
	if config.LogLevel != "info" {
		t.Errorf("Expected default log level 'info', got '%s'", config.LogLevel)
	}
}

func TestGetFastlyConfigInvalidPath(t *testing.T) {
	// Arrange
	configPath := "/nonexistent/path/to/config.json"

	// Act
	config, err := GetFastlyConfig(configPath)

	// Assert
	if err == nil {
		t.Error("Expected error for nonexistent config file, got nil")
	}
	if config != nil {
		t.Error("Expected nil config for invalid path")
	}
}

func TestGetFastlyConfigInvalidJSON(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	invalidJSON := `{ invalid json }`
	err := os.WriteFile(configPath, []byte(invalidJSON), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	// Act
	config, err := GetFastlyConfig(configPath)

	// Assert
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
	if config != nil {
		t.Error("Expected nil config for invalid JSON")
	}
}

func TestGetFastlyConfigMissingAPIKey(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	configData := `{
        "log_level": "debug"
    }`
	err := os.WriteFile(configPath, []byte(configData), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	// Act
	config, err := GetFastlyConfig(configPath)

	// Assert
	if err == nil {
		t.Error("Expected error for missing API key, got nil")
	}
	if config != nil {
		t.Error("Expected nil config when API key is missing")
	}
	if err != nil && err.Error() != "Fastly API key is required but not provided in config" {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

func TestGetFastlyConfigEmptyAPIKey(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	configData := `{
        "fastly_api_key": "",
        "log_level": "debug"
    }`
	err := os.WriteFile(configPath, []byte(configData), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	// Act
	config, err := GetFastlyConfig(configPath)

	// Assert
	if err == nil {
		t.Error("Expected error for empty API key, got nil")
	}
	if config != nil {
		t.Error("Expected nil config when API key is empty")
	}
}

func TestGetFastlyConfigDefaultHTTPTimeout(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	configData := `{
        "fastly_api_key": "test-api-key"
    }`
	err := os.WriteFile(configPath, []byte(configData), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	// Act
	config, err := GetFastlyConfig(configPath)

	// Assert
	if err != nil {
		t.Fatalf("GetFastlyConfig returned error: %v", err)
	}
	if config.HTTPTimeoutSec != 30 {
		t.Errorf("Expected default HTTP timeout 30 seconds, got %d", config.HTTPTimeoutSec)
	}
}

func TestGetFastlyConfigCustomHTTPTimeout(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	configData := `{
        "fastly_api_key": "test-api-key",
        "http_timeout_sec": 60
    }`
	err := os.WriteFile(configPath, []byte(configData), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	// Act
	config, err := GetFastlyConfig(configPath)

	// Assert
	if err != nil {
		t.Fatalf("GetFastlyConfig returned error: %v", err)
	}
	if config.HTTPTimeoutSec != 60 {
		t.Errorf("Expected HTTP timeout 60 seconds, got %d", config.HTTPTimeoutSec)
	}
}

func TestGetFastlyConfigInvalidHTTPTimeout(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	configData := `{
        "fastly_api_key": "test-api-key",
        "http_timeout_sec": 500
    }`
	err := os.WriteFile(configPath, []byte(configData), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	// Act
	config, err := GetFastlyConfig(configPath)

	// Assert
	if err == nil {
		t.Error("Expected error for timeout > 300 seconds, got nil")
	}
	if config != nil {
		t.Error("Expected nil config for invalid timeout")
	}
	if err != nil && !contains(err.Error(), "HTTP timeout must be between 1 and 300 seconds") {
		t.Errorf("Expected timeout validation error, got: %v", err)
	}
}

func TestGetFastlyConfigNegativeHTTPTimeout(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	configData := `{
        "fastly_api_key": "test-api-key",
        "http_timeout_sec": -10
    }`
	err := os.WriteFile(configPath, []byte(configData), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	// Act
	config, err := GetFastlyConfig(configPath)

	// Assert - negative values should default to 30
	if err != nil {
		t.Fatalf("GetFastlyConfig returned error: %v", err)
	}
	if config.HTTPTimeoutSec != 30 {
		t.Errorf("Expected default HTTP timeout 30 seconds for negative value, got %d", config.HTTPTimeoutSec)
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
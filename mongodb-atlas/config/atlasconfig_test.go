package config

import (
	"os"
	"testing"
)

// Unit tests for the GetAtlasConfig function
func TestGetAtlasConfig(t *testing.T) {
	// Test: Valid configuration file
	// t.Run("Valid configuration file", func(t *testing.T) {
	// 	configFilePath := "test_valid_config.json"
	// 	// Create a temporary valid JSON file
	// 	validConfig := `{"log_level": "debug"}`
	// 	err := os.WriteFile(configFilePath, []byte(validConfig), 0644)
	// 	if err != nil {
	// 		t.Fatalf("failed to create temporary config file: %v", err)
	// 	}
	// 	defer os.Remove(configFilePath)

	// 	config, err := GetAtlasConfig(configFilePath)
	// 	if err != nil {
	// 		t.Fatalf("expected no error, but got: %v", err)
	// 	}
	// 	fmt.Println(config, configFilePath)
	// 	if config.LogLevel != "debug" {
	// 		t.Errorf("expected log level to be 'debug', but got: %s", config.LogLevel)
	// 	}
	// })

	// Test: Invalid file path
	t.Run("Invalid file path", func(t *testing.T) {
		configFilePath := "invalid_path.json"
		_, err := GetAtlasConfig(configFilePath)
		if err == nil {
			t.Errorf("expected an error, but got none")
		}
	})

	// Test: Invalid JSON format
	t.Run("Invalid JSON format", func(t *testing.T) {
		configFilePath := "test_invalid_json.json"
		// Create a temporary invalid JSON file
		invalidConfig := `{"log_level": "debug"`
		err := os.WriteFile(configFilePath, []byte(invalidConfig), 0644)
		if err != nil {
			t.Fatalf("failed to create temporary config file: %v", err)
		}
		defer os.Remove(configFilePath)

		_, err = GetAtlasConfig(configFilePath)
		if err == nil {
			t.Errorf("expected an error, but got none")
		}
	})

	// Test: Default log level when missing
	t.Run("Default log level when missing", func(t *testing.T) {
		configFilePath := "test_missing_log_level.json"
		// Create a temporary JSON file without log_level
		missingLogLevelConfig := `{}`
		err := os.WriteFile(configFilePath, []byte(missingLogLevelConfig), 0644)
		if err != nil {
			t.Fatalf("failed to create temporary config file: %v", err)
		}
		defer os.Remove(configFilePath)

		config, err := GetAtlasConfig(configFilePath)
		if err != nil {
			t.Fatalf("expected no error, but got: %v", err)
		}
		if config.LogLevel != "info" {
			t.Errorf("expected log level to be 'info', but got: %s", config.LogLevel)
		}
	})
}

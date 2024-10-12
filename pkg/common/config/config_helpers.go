package config

import (
	"fmt"
	"os"
)

func GetConfigFilePath() (string, error) {
	// plugins expect exactly 2 args: the executable itself,
	// and a path to the config file to use
	// all config for the plugin must come through the config file
	if len(os.Args) != 2 {
		return "", fmt.Errorf("plugins require 2 args: the plugin itself, and the full path to its config file. Got %d args", len(os.Args))
	}

	_, err := os.Stat(os.Args[1])
	if err != nil {
		return "", fmt.Errorf("error reading config file at %s: %v", os.Args[1], err)
	}

	return os.Args[1], nil
}

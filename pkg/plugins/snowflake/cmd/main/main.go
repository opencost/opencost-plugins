package main

import (
	"os"

	"github.com/hashicorp/go-plugin"
	snowflakeconfig "github.com/opencost/opencost-plugins/pkg/plugins/snowflake/config"
	snowflakeplugin "github.com/opencost/opencost-plugins/pkg/plugins/snowflake/plugin"
	"github.com/opencost/opencost/core/pkg/log"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
	_ "github.com/snowflakedb/gosnowflake"
)

var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "snowflake",
}

func main() {
	log.Debug("Initializing Snowflake plugin")

	if len(os.Args) != 2 {
		log.Fatalf("plugins require 2 args: the plugin itself, and the full path to its config file. Got %d args", len(os.Args))
	}

	configPath := os.Args[1]
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Fatalf("config file does not exist: %s", configPath)
	}

	cfg, err := snowflakeconfig.GetSnowflakeConfig(configPath)
	if err != nil {
		log.Fatalf("error building Snowflake config: %v", err)
	}
	log.SetLogLevel(cfg.LogLevel)

	costSource, err := snowflakeplugin.NewSnowflakeCostSource(cfg)
	if err != nil {
		log.Fatalf("error creating Snowflake cost source: %v", err)
	}

	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: costSource},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

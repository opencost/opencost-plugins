package genaiprovider

import (
	"context"
	"encoding/json"
	"os"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// CustomCostPlugin is the implementation of the plugin.Plugin interface.
type CustomCostPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	Impl CustomCostSource
}

func (p *CustomCostPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	// Register your gRPC server here using the generated protobuf server
	RegisterCustomCostSourceServer(s, &GRPCServer{Impl: p.Impl})
	return nil
}

func (p *CustomCostPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &GRPCClient{client: NewCustomCostSourceClient(c)}, nil
}

// Config represents the configuration for the GenAI plugin
type Config struct {
	PrometheusURL string `json:"prometheus_url"`
	OpenCostURL   string `json:"opencost_url"`
	ClusterID     string `json:"cluster_id"`
	LogLevel      string `json:"log_level"`
}

// LoadConfig loads the configuration from a JSON file
func LoadConfig(configPath string) (*Config, error) {
	var config Config
	bytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(bytes, &config)
	if err != nil {
		return nil, err
	}

	if config.LogLevel == "" {
		config.LogLevel = "info"
	}
	if config.OpenCostURL == "" {
		config.OpenCostURL = "http://localhost:9003"
	}

	return &config, nil
}

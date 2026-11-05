package ocostsplitterplugin

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the plugin configuration, read from the JSON config file that
// OpenCost passes to the plugin on launch.
type Config struct {
	// OpencostAPIAddress is the address of the OpenCost API the plugin queries
	// for allocation data. In-cluster default: http://opencost.opencost:9003
	OpencostAPIAddress string `json:"opencost_api_address"`

	// SplitMethod is one of: proportional, equal, weighted, none.
	SplitMethod string `json:"split_method"`

	// SharedNamespaces are the namespaces whose cost is treated as the shared
	// pool. Defaults to the common shared-infra namespaces.
	SharedNamespaces []string `json:"shared_namespaces"`

	// WeightsFile is an optional CSV (namespace,weight) used by the weighted
	// method. Weights are typically revenue or headcount shares agreed with
	// finance, in writing.
	WeightsFile string `json:"weights_file"`

	// Currency is the FOCUS billing currency. Default: USD.
	Currency string `json:"currency"`

	// LogLevel: debug, info, warn, error. Default: info.
	LogLevel string `json:"log_level"`
}

// DefaultSharedNamespaces are the classic shared-infra namespaces. Teams
// override this list in their config to match their own estate.
var DefaultSharedNamespaces = []string{"kube-system", "monitoring", "ingress-nginx"}

// LoadWeights reads an optional CSV file of namespace,weight pairs.
// Missing namespaces are not in the map; the caller decides the fallback.
func LoadWeights(path string) (map[string]float64, error) {
	if path == "" {
		return map[string]float64{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read weights file: %w", err)
	}
	reader := csv.NewReader(strings.NewReader(string(raw)))
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse weights file: %w", err)
	}
	out := map[string]float64{}
	for i, row := range rows {
		if i == 0 && len(row) >= 1 && strings.EqualFold(strings.TrimSpace(row[0]), "namespace") {
			continue // header
		}
		if len(row) < 2 {
			return nil, fmt.Errorf("weights file line %d: expected namespace,weight", i+1)
		}
		ns := strings.TrimSpace(row[0])
		w, err := strconv.ParseFloat(strings.TrimSpace(row[1]), 64)
		if err != nil {
			return nil, fmt.Errorf("weights file line %d: bad weight %q for namespace %s", i+1, row[1], ns)
		}
		if ns == "" {
			return nil, fmt.Errorf("weights file line %d: empty namespace", i+1)
		}
		out[ns] = w
	}
	return out, nil
}

// IsShared reports whether the namespace is in the configured shared list.
func (c *Config) IsShared(namespace string) bool {
	for _, ns := range c.SharedNamespaces {
		if strings.EqualFold(strings.TrimSpace(ns), namespace) {
			return true
		}
	}
	return false
}

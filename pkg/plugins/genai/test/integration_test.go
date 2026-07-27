package test

import (
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/opencost/opencost-plugins/pkg/plugins/genai/genaiprovider"
)

func TestGenAIPlugin(t *testing.T) {
	// 1. Point to the binary you just built
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  1,
			MagicCookieKey:   "OPENCOST_PLUGIN_MAGIC_COOKIE",
			MagicCookieValue: "genai-visibility",
		},
		Plugins: map[string]plugin.Plugin{
			"customcost": &genaiprovider.CustomCostPlugin{},
		},
		Cmd: exec.Command("../genai-plugin"), // Adjust path to your binary
	})
	defer client.Kill()

	// 2. Connect to the plugin
	rpcClient, _ := client.Client()
	raw, _ := rpcClient.Dispense("customcost")
	source := raw.(genaiprovider.CustomCostSource)

	// 3. Request a 24-hour window
	now := time.Now()
	resp, err := source.GetCustomCosts(&genaiprovider.CustomCostRequest{
		Start:      now.Add(-24 * time.Hour),
		End:        now,
		Resolution: "hourly",
	})

	if err != nil {
		t.Fatalf("Failed to get costs: %v", err)
	}

	// 4. Validate the Data
	fmt.Printf("Received %d GenAI workloads\n", len(resp))
	for _, w := range resp {
		fmt.Printf("Pod: %s | Cost: $%.2f | Model: %s | Tokens: %d\n", w.PodName, w.TotalCost, w.ModelName, w.TotalTokens)
	}
}

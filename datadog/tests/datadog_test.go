package tests

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	datadogplugin "github.com/opencost/opencost-plugins/datadog/datadogplugin"
	harness "github.com/opencost/opencost-plugins/test/pkg/harness"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model"
	"github.com/opencost/opencost/core/pkg/opencost"
)

// this test gets DD keys from env vars, writes a config file
// then it invokes the plugin and validates some basics
// in the response
func TestDDCostRetrieval(t *testing.T) {
	// read necessary env vars. If any are missing, log warning and skip test
	ddSite := os.Getenv("DD_SITE")
	ddApiKey := os.Getenv("DD_API_KEY")
	ddAppKey := os.Getenv("DD_APPLICATION_KEY")

	if ddSite == "" {
		log.Warnf("DD_SITE undefined, this needs to have the URL of your DD instance, skipping test")
		t.Skip()
		return
	}

	if ddApiKey == "" {
		log.Warnf("DD_API_KEY undefined, skipping test")
		t.Skip()
		return
	}

	if ddAppKey == "" {
		log.Warnf("DD_APPLICATION_KEY undefined, skipping test")
		t.Skip()
		return
	}

	// write out config to temp file using contents of env vars
	config := datadogplugin.DatadogConfig{
		DDSite:   ddSite,
		DDAPIKey: ddApiKey,
		DDAppKey: ddAppKey,
	}

	// set up custom cost request
	file, err := os.CreateTemp("", "datadog_config.json")
	if err != nil {
		t.Fatalf("could not create temp config dir: %v", err)
	}
	defer os.Remove(file.Name())
	data, err := json.MarshalIndent(config, "", " ")
	if err != nil {
		t.Fatalf("could not marshal json: %v", err)
	}

	err = os.WriteFile(file.Name(), data, fs.FileMode(os.O_RDWR))
	if err != nil {
		t.Fatalf("could not write file: %v", err)
	}

	// invoke plugin via harness
	_, filename, _, _ := runtime.Caller(0)
	parent := filepath.Dir(filename)
	pluginRoot := filepath.Dir(parent)
	pluginFile := pluginRoot + "/cmd/main/main.go"
	windowStart := time.Date(2024, 2, 27, 0, 0, 0, 0, time.UTC)
	// query for qty 2 of 1 hour windows
	windowEnd := time.Date(2024, 2, 27, 2, 0, 0, 0, time.UTC)
	win := opencost.NewClosedWindow(windowStart, windowEnd)
	req := model.CustomCostRequest{
		TargetWindow: &win,
		Resolution:   time.Hour,
	}
	response := harness.InvokePlugin(file.Name(), pluginFile, req)

	// confirm no errors in result
	if len(response) == 0 {
		t.Fatalf("empty response")
	}
	for _, resp := range response {
		if len(resp.Errors) > 0 {
			t.Fatalf("got errors in response: %v", resp.Errors)
		}
	}

	result, err := json.MarshalIndent(response, "", "    ")
	if err != nil {
		t.Fatalf("error json-ing response: %v", err)
	}

	fmt.Println(string(result))
	// confirm results have correct provider
	for _, resp := range response {
		if resp.Domain != "datadog" {
			t.Fatalf("unexpected domain. expected datadog, got %s", resp.Domain)
		}
	}
	// confirm there are > 0 custom costs
	for _, resp := range response {
		if len(resp.Costs) < 1 {
			t.Fatalf("expect non-zero costs in response.")
		}
	}

}

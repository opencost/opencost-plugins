package tests

import (
	"encoding/json"
	"fmt"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	datadogplugin "github.com/opencost/opencost-plugins/datadog/datadogplugin"
	harness "github.com/opencost/opencost-plugins/test/pkg/harness"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDDCostRetrievalListCost(t *testing.T) {
	// query for qty 2 of 1 hour windows
	windowStart := time.Date(2024, 3, 8, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2024, 3, 8, 2, 0, 0, 0, time.UTC)

	response := getResponse(t, windowStart, windowEnd, time.Hour)

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

	t.Log(string(result))
	// confirm results have correct provider
	for _, resp := range response {
		if resp.Domain != "datadog" {
			t.Fatalf("unexpected domain. expected datadog, got %s", resp.Domain)
		}
	}

	// check some attributes of the cost response
	for _, resp := range response {
		// confirm there are > 0 custom costs
		if len(resp.Costs) < 1 {
			t.Fatalf("expect non-zero costs in response.")
		}

		for _, cost := range resp.Costs {
			if cost.ResourceType == "indexed_logs" {
				// check for sane values fo a rate-priced resource
				if cost.ListCost > 1000 {
					costDump := spew.Sdump(cost)
					t.Log(costDump)
					t.Fatalf("unexpectedly high cost for indexed logs: %f", cost.ListCost)
				}
			}
		}
	}
}

func TestFuturism(t *testing.T) {
	// query for the future
	windowStart := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	windowEnd := windowStart.Add(time.Hour)

	response := getResponse(t, windowStart, windowEnd, time.Hour)

	// when we query for data in the future, we expect to get back no data AND no errors
	if len(response) > 0 {
		t.Fatalf("got non-empty response")
	}
	for _, resp := range response {
		if len(resp.Errors) > 0 {
			t.Fatalf("got errors in response: %v", resp.Errors)
		}
	}
}

func TestDDCostRetrievalBilledCost(t *testing.T) {
	// query for qty 2 of 1 hour windows
	windowStart := time.Date(2024, 3, 16, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2024, 3, 17, 0, 0, 0, 0, time.UTC)

	response := getResponse(t, windowStart, windowEnd, timeutil.Day)

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

	// check some attributes of the cost response
	var totalBilledCost float32 = 0.0
	for _, resp := range response {
		// confirm there are > 0 custom costs
		if len(resp.Costs) < 1 {
			t.Fatalf("expect non-zero costs in response.")
		}

		for _, cost := range resp.Costs {
			totalBilledCost += cost.BilledCost
		}
	}

	if totalBilledCost == 0 {
		responseDump := spew.Sdump(response)
		t.Log(responseDump)
		t.Fatalf("unexpectedly low total billed cost: %f", totalBilledCost)
	}
}

func getResponse(t *testing.T, windowStart, windowEnd time.Time, step time.Duration) []*pb.CustomCostResponse {
	// read necessary env vars. If any are missing, log warning and skip test
	ddSite := os.Getenv("DD_SITE")
	ddApiKey := os.Getenv("DD_API_KEY")
	ddAppKey := os.Getenv("DD_APPLICATION_KEY")

	if ddSite == "" {
		log.Warnf("DD_SITE undefined, this needs to have the URL of your DD instance, skipping test")
		t.Skip()
		return nil
	}

	if ddApiKey == "" {
		log.Warnf("DD_API_KEY undefined, skipping test")
		t.Skip()
		return nil
	}

	if ddAppKey == "" {
		log.Warnf("DD_APPLICATION_KEY undefined, skipping test")
		t.Skip()
		return nil
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

	req := pb.CustomCostRequest{
		Start:      timestamppb.New(windowStart),
		End:        timestamppb.New(windowEnd),
		Resolution: durationpb.New(step),
	}
	return harness.InvokePlugin(file.Name(), pluginFile, &req)
}

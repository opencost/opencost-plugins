package tests

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/opencost/opencost-plugins/pkg/plugins/fastly/fastlyplugin"
	"github.com/opencost/opencost-plugins/test/pkg/harness"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestFastlyCostRetrieval(t *testing.T) {
	// Query for last month's data
	now := time.Now()
	windowStart := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	response := getFastlyResponse(t, windowStart, windowEnd, timeutil.Day)

	// confirm no errors in result
	if len(response) == 0 {
		t.Fatalf("empty response")
	}
	for _, resp := range response {
		if len(resp.Errors) > 0 {
			t.Fatalf("got errors in response: %v", resp.Errors)
		}
	}

	// confirm results have correct provider
	for _, resp := range response {
		if resp.Domain != "fastly" {
			t.Fatalf("unexpected domain. expected fastly, got %s", resp.Domain)
		}
	}

	// check some attributes of the cost response
	totalCosts := 0
	totalBilled := float32(0)
	for _, resp := range response {
		// May have zero costs for some days
		totalCosts += len(resp.Costs)

		for _, cost := range resp.Costs {
			totalBilled += cost.BilledCost

			// Verify required fields are populated
			if cost.ProviderId == "" {
				t.Errorf("empty ProviderId")
			}
			if cost.ResourceType == "" {
				t.Errorf("empty ResourceType")
			}
			if cost.ResourceName == "" {
				t.Errorf("empty ResourceName")
			}
		}
	}

	t.Logf("Total responses: %d, Total costs: %d, Total billed: %.2f", len(response), totalCosts, totalBilled)
}

func TestFastlyFutureWindow(t *testing.T) {
	// query for the future
	windowStart := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	windowEnd := windowStart.Add(time.Hour)

	response := getFastlyResponse(t, windowStart, windowEnd, time.Hour)

	// when we query for data in the future, we expect to get back no data AND no errors
	if len(response) > 0 {
		t.Fatalf("got non-empty response for future window")
	}
}

func TestFastlyHourlyData(t *testing.T) {
	// query for hourly data from yesterday
	now := time.Now()
	windowStart := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(24 * time.Hour)

	response := getFastlyResponse(t, windowStart, windowEnd, time.Hour)

	// Should get 24 responses for hourly data
	if len(response) != 24 {
		t.Errorf("expected 24 hourly responses, got %d", len(response))
	}

	// Verify each response
	for i, resp := range response {
		if len(resp.Errors) > 0 {
			t.Errorf("errors in hourly response %d: %v", i, resp.Errors)
		}

		// Verify timestamps
		expectedStart := windowStart.Add(time.Duration(i) * time.Hour)
		expectedEnd := expectedStart.Add(time.Hour)

		if !resp.Start.AsTime().Equal(expectedStart) {
			t.Errorf("response %d: expected start %v, got %v", i, expectedStart, resp.Start.AsTime())
		}
		if !resp.End.AsTime().Equal(expectedEnd) {
			t.Errorf("response %d: expected end %v, got %v", i, expectedEnd, resp.End.AsTime())
		}
	}
}

func TestFastlyMonthToDate(t *testing.T) {
	// Query for current month to date
	now := time.Now()
	windowStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	windowEnd := now.Truncate(24 * time.Hour)

	response := getFastlyResponse(t, windowStart, windowEnd, timeutil.Day)

	if len(response) == 0 {
		t.Fatalf("empty response for month-to-date")
	}

	// Should include current month data
	foundCurrentMonth := false
	for _, resp := range response {
		if resp.Start.AsTime().Month() == now.Month() {
			foundCurrentMonth = true
			break
		}
	}

	if !foundCurrentMonth {
		t.Errorf("no data found for current month")
	}
}

func getFastlyResponse(t *testing.T, windowStart, windowEnd time.Time, step time.Duration) []*pb.CustomCostResponse {
	// read necessary env vars. If any are missing, log warning and skip test
	fastlyAPIKey := os.Getenv("FASTLY_API_KEY")

	if fastlyAPIKey == "" {
		log.Warnf("FASTLY_API_KEY undefined, skipping test")
		t.Skip()
		return nil
	}

	// write out config to temp file using contents of env vars
	config := fastlyplugin.FastlyConfig{
		FastlyAPIKey: fastlyAPIKey,
		LogLevel:     "debug",
	}

	// set up custom cost request and write to temp file
	file, err := os.CreateTemp("", "fastly_config.json")
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

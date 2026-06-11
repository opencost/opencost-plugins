package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coralogixplugin "github.com/opencost/opencost-plugins/pkg/plugins/coralogix/coralogixplugin"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestGetCustomCostsFetchesDataUsage(t *testing.T) {
	var requestCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.URL.Path != "/dataplans/data-usage/v2" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "test-key" {
			t.Fatalf("unexpected authorization header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("unexpected accept header: %s", r.Header.Get("Accept"))
		}
		if r.URL.Query().Get("fromDate") != "2026-05-01T00:00:00.000Z" {
			t.Fatalf("unexpected fromDate: %s", r.URL.Query().Get("fromDate"))
		}
		if r.URL.Query().Get("toDate") != "2026-05-02T00:00:00.000Z" {
			t.Fatalf("unexpected toDate: %s", r.URL.Query().Get("toDate"))
		}
		if got := r.URL.Query()["aggregate"]; len(got) != 2 || got[0] != "AGGREGATE_BY_APPLICATION" || got[1] != "AGGREGATE_BY_SUBSYSTEM" {
			t.Fatalf("unexpected aggregate query: %v", got)
		}

		fmt.Fprint(w, `{
			"entries": [
				{
					"timestamp": "2026-05-01T00:00:00Z",
					"sizeGb": 10.5,
					"units": 21,
					"dimensions": [
						{"genericDimension": {"key": "application", "value": "checkout"}},
						{"genericDimension": {"key": "subsystem", "value": "api"}},
						{"pillar": "PILLAR_LOGS"},
						{"priority": "PRIORITY_HIGH"}
					]
				}
			]
		}`)
	}))
	defer server.Close()

	source := CoralogixCostSource{
		client: server.Client(),
		config: &coralogixplugin.CoralogixConfig{
			APIKey:          "test-key",
			APIBaseURL:      server.URL,
			DateRangeStyle:  coralogixplugin.DateRangeStyleForm,
			AggregateBy:     []string{"AGGREGATE_BY_APPLICATION", "AGGREGATE_BY_SUBSYSTEM"},
			SizeGBUnitPrice: 0.50,
			UnitPrice:       0.25,
		},
	}

	resp := source.GetCustomCosts(customCostRequest())
	if len(resp) != 1 {
		t.Fatalf("expected one response, got %d", len(resp))
	}
	if len(resp[0].Errors) != 0 {
		t.Fatalf("unexpected errors: %v", resp[0].Errors)
	}
	if len(resp[0].Costs) != 2 {
		t.Fatalf("expected two custom costs, got %d", len(resp[0].Costs))
	}

	gbCost := resp[0].Costs[0]
	if gbCost.BilledCost != 5.25 {
		t.Fatalf("unexpected GB billed cost: %f", gbCost.BilledCost)
	}
	if gbCost.UsageQuantity != 10.5 {
		t.Fatalf("unexpected GB usage quantity: %f", gbCost.UsageQuantity)
	}
	if gbCost.UsageUnit != "GB" {
		t.Fatalf("unexpected GB usage unit: %s", gbCost.UsageUnit)
	}
	if gbCost.AccountName != "checkout" {
		t.Fatalf("unexpected account name: %s", gbCost.AccountName)
	}
	if gbCost.ResourceName != "checkout/api" {
		t.Fatalf("unexpected resource name: %s", gbCost.ResourceName)
	}
	if gbCost.ResourceType != "logs" {
		t.Fatalf("unexpected resource type: %s", gbCost.ResourceType)
	}
	if gbCost.ExtendedAttributes.GetProvider() != "CoraLogix" {
		t.Fatalf("unexpected provider: %s", gbCost.ExtendedAttributes.GetProvider())
	}
	if !strings.Contains(gbCost.Description, "application=checkout") {
		t.Fatalf("description missing dimensions: %s", gbCost.Description)
	}

	unitCost := resp[0].Costs[1]
	if unitCost.BilledCost != 5.25 {
		t.Fatalf("unexpected unit billed cost: %f", unitCost.BilledCost)
	}
	if unitCost.UsageQuantity != 21 {
		t.Fatalf("unexpected unit usage quantity: %f", unitCost.UsageQuantity)
	}
	if unitCost.UsageUnit != "unit" {
		t.Fatalf("unexpected unit usage unit: %s", unitCost.UsageUnit)
	}
	if requestCount != 1 {
		t.Fatalf("expected one request, got %d", requestCount)
	}
}

func TestParseDataUsageResponseAcceptsNDJSONAndSSE(t *testing.T) {
	payload := strings.NewReader(`
data: {"timestamp":"2026-05-01T00:00:00Z","sizeGb":2,"dimensions":[{"pillar":"PILLAR_METRICS"}]}
{"entries":[{"timestamp":"2026-05-01T00:00:00Z","units":3,"dimensions":[{"genericDimension":{"key":"dataset","value":"default"}}]}]}
`)

	entries, err := parseDataUsageResponse(payload)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected two entries, got %d", len(entries))
	}
	if entries[0].SizeGB != 2 {
		t.Fatalf("unexpected first sizeGb: %f", entries[0].SizeGB)
	}
	if entries[1].Units != 3 {
		t.Fatalf("unexpected second units: %f", entries[1].Units)
	}
}

func TestGetCustomCostsRejectsHourlyResolution(t *testing.T) {
	source := CoralogixCostSource{
		client: http.DefaultClient,
		config: &coralogixplugin.CoralogixConfig{},
	}
	req := customCostRequest()
	req.Resolution = durationpb.New(time.Hour)

	resp := source.GetCustomCosts(req)
	if len(resp) != 1 {
		t.Fatalf("expected one response, got %d", len(resp))
	}
	if len(resp[0].Errors) != 1 {
		t.Fatalf("expected one error, got %v", resp[0].Errors)
	}
}

func customCostRequest() *pb.CustomCostRequest {
	windowStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	return &pb.CustomCostRequest{
		Start:      timestamppb.New(windowStart),
		End:        timestamppb.New(windowEnd),
		Resolution: durationpb.New(timeutil.Day),
	}
}

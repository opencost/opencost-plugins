package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	newrelicplugin "github.com/opencost/opencost-plugins/pkg/plugins/newrelic/newrelicplugin"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestGetCustomCostsFetchesNewRelicUsage(t *testing.T) {
	var requestCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.Header.Get("API-Key") != "test-key" {
			t.Fatalf("unexpected API-Key header: %s", r.Header.Get("API-Key"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected content type: %s", r.Header.Get("Content-Type"))
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("unexpected request body: %v", err)
		}
		query := body["query"]
		if !strings.Contains(query, "account(id: 12345)") {
			t.Fatalf("request query missing account id: %s", query)
		}
		if !strings.Contains(query, "SINCE '2026-05-01 00:00:00 UTC'") {
			t.Fatalf("request query missing rendered start: %s", query)
		}
		if !strings.Contains(query, "UNTIL '2026-05-02 00:00:00 UTC'") {
			t.Fatalf("request query missing rendered end: %s", query)
		}

		fmt.Fprint(w, `{
			"data": {
				"actor": {
					"account": {
						"nrql": {
							"results": [
								{"facet": ["logs", 67890], "usageQuantity": 10.5, "beginTimeSeconds": 1777593600, "endTimeSeconds": 1777680000},
								{"facet": ["metrics", 67890], "usageQuantity": 0}
							]
						}
					}
				}
			}
		}`)
	}))
	defer server.Close()

	source := NewRelicCostSource{
		client: server.Client(),
		config: &newrelicplugin.NewRelicConfig{
			APIKey:       "test-key",
			AccountID:    12345,
			NerdGraphURL: server.URL,
			AccountName:  "parent-account",
			Currency:     "USD",
			UsageQueries: []newrelicplugin.UsageQueryConfig{
				{
					Name:              newrelicplugin.MetricDataIngest,
					NRQL:              "FROM NrConsumption SELECT sum(GigabytesIngested) AS usageQuantity WHERE productLine = 'DataPlatform' SINCE '{start}' UNTIL '{end}' FACET usageMetric, consumingAccountId",
					QuantityAttribute: newrelicplugin.DefaultQuantityAttribute,
					Unit:              "GB",
					UnitPrice:         0.4,
					PriceKey:          newrelicplugin.MetricDataIngest,
					ResourceType:      "DataPlatform",
					FacetAttributes:   []string{"usageMetric", "consumingAccountId"},
				},
			},
		},
	}

	resp := source.GetCustomCosts(customCostRequest())
	if len(resp) != 1 {
		t.Fatalf("expected one response, got %d", len(resp))
	}
	if len(resp[0].Errors) != 0 {
		t.Fatalf("unexpected errors: %v", resp[0].Errors)
	}
	if len(resp[0].Costs) != 1 {
		t.Fatalf("expected one custom cost, got %d", len(resp[0].Costs))
	}

	cost := resp[0].Costs[0]
	if cost.BilledCost != 4.2 {
		t.Fatalf("unexpected billed cost: %f", cost.BilledCost)
	}
	if cost.UsageQuantity != 10.5 {
		t.Fatalf("unexpected usage quantity: %f", cost.UsageQuantity)
	}
	if cost.UsageUnit != "GB" {
		t.Fatalf("unexpected usage unit: %s", cost.UsageUnit)
	}
	if cost.AccountName != "67890" {
		t.Fatalf("unexpected account name: %s", cost.AccountName)
	}
	if cost.ResourceName != "logs" {
		t.Fatalf("unexpected resource name: %s", cost.ResourceName)
	}
	if cost.ResourceType != "DataPlatform" {
		t.Fatalf("unexpected resource type: %s", cost.ResourceType)
	}
	if cost.ExtendedAttributes.GetProvider() != "New Relic" {
		t.Fatalf("unexpected provider: %s", cost.ExtendedAttributes.GetProvider())
	}
	if !strings.Contains(cost.Description, "usageMetric=logs") {
		t.Fatalf("description missing dimensions: %s", cost.Description)
	}
	if requestCount != 1 {
		t.Fatalf("expected one request, got %d", requestCount)
	}
}

func TestGetCustomCostsReturnsNerdGraphErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"errors":[{"message":"invalid NRQL"}]}`)
	}))
	defer server.Close()

	source := NewRelicCostSource{
		client: server.Client(),
		config: &newrelicplugin.NewRelicConfig{
			APIKey:       "test-key",
			AccountID:    12345,
			NerdGraphURL: server.URL,
			AccountName:  "parent-account",
			Currency:     "USD",
			UsageQueries: []newrelicplugin.UsageQueryConfig{
				{
					Name:              "bad_query",
					NRQL:              "SELECT bad",
					QuantityAttribute: newrelicplugin.DefaultQuantityAttribute,
					Unit:              "unit",
				},
			},
		},
	}

	resp := source.GetCustomCosts(customCostRequest())
	if len(resp) != 1 {
		t.Fatalf("expected one response, got %d", len(resp))
	}
	if len(resp[0].Errors) != 1 {
		t.Fatalf("expected one error, got %v", resp[0].Errors)
	}
	if !strings.Contains(resp[0].Errors[0], "invalid NRQL") {
		t.Fatalf("unexpected error: %s", resp[0].Errors[0])
	}
}

func TestGetCustomCostsRejectsHourlyResolution(t *testing.T) {
	source := NewRelicCostSource{
		client: http.DefaultClient,
		config: &newrelicplugin.NewRelicConfig{},
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

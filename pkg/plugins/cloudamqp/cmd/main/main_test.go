package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cloudamqpplugin "github.com/opencost/opencost-plugins/pkg/plugins/cloudamqp/cloudamqpplugin"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestGetCustomCostsProratesMonthlyInvoiceLines(t *testing.T) {
	var requestCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.URL.Path != "/api/invoices/period" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "" || pass != "test-key" {
			t.Fatalf("unexpected basic auth: %q %q %t", user, pass, ok)
		}
		if r.URL.Query().Get("year") != "2026" || r.URL.Query().Get("month") != "5" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(invoiceResponse{
			Period:   "2026-05",
			Total:    62,
			Currency: "USD",
			Customer: map[string]interface{}{
				"name": "Example Co",
			},
			Lines: []map[string]interface{}{
				{
					"id":            "line-1",
					"description":   "Little Lemur",
					"amount":        31.0,
					"quantity":      1.0,
					"unit":          "instance-month",
					"instance_name": "rabbit-prod",
					"plan":          "lemur",
				},
				{
					"id":          "line-2",
					"description": "Support",
					"amount":      31.0,
				},
			},
		})
	}))
	defer server.Close()

	source := CloudAMQPCostSource{
		client: server.Client(),
		config: &cloudamqpplugin.CloudAMQPConfig{
			APIKey:      "test-key",
			APIBaseURL:  server.URL + "/api",
			AccountName: "fallback",
			Currency:    "USD",
		},
	}

	resp := source.GetCustomCosts(customCostRequest())
	if len(resp) != 2 {
		t.Fatalf("expected two responses, got %d", len(resp))
	}
	if requestCount != 1 {
		t.Fatalf("expected monthly invoice to be cached, got %d requests", requestCount)
	}
	if len(resp[0].Errors) != 0 {
		t.Fatalf("unexpected errors: %v", resp[0].Errors)
	}
	if len(resp[0].Costs) != 2 {
		t.Fatalf("expected two costs, got %d", len(resp[0].Costs))
	}

	first := resp[0].Costs[0]
	if first.BilledCost != 1 {
		t.Fatalf("unexpected prorated billed cost: %f", first.BilledCost)
	}
	if first.AccountName != "Example Co" {
		t.Fatalf("unexpected account name: %s", first.AccountName)
	}
	if first.ResourceName != "rabbit-prod" {
		t.Fatalf("unexpected resource name: %s", first.ResourceName)
	}
	if first.ResourceType != "lemur" {
		t.Fatalf("unexpected resource type: %s", first.ResourceType)
	}
	if first.ExtendedAttributes.GetChargeFrequency() != "Monthly" {
		t.Fatalf("unexpected charge frequency: %s", first.ExtendedAttributes.GetChargeFrequency())
	}
}

func TestGetCustomCostsRejectsHourlyResolution(t *testing.T) {
	source := CloudAMQPCostSource{
		client: http.DefaultClient,
		config: &cloudamqpplugin.CloudAMQPConfig{},
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

func TestInvoiceWithoutLineAmountsFallsBackToInvoiceTotal(t *testing.T) {
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	window := opencostWindow(start, start.AddDate(0, 0, 1))
	invoice := &invoiceResponse{
		Period:   "2026-05",
		Total:    31,
		Currency: "USD",
		Lines: []map[string]interface{}{
			{"description": "No amount"},
		},
	}

	costs := cloudAMQPInvoiceToCustomCosts(&cloudamqpplugin.CloudAMQPConfig{AccountName: "acct", Currency: "USD"}, invoice, window)
	if len(costs) != 1 {
		t.Fatalf("expected fallback cost, got %d", len(costs))
	}
	if costs[0].BilledCost != 1 {
		t.Fatalf("unexpected fallback cost: %f", costs[0].BilledCost)
	}
}

func customCostRequest() *pb.CustomCostRequest {
	windowStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
	return &pb.CustomCostRequest{
		Start:      timestamppb.New(windowStart),
		End:        timestamppb.New(windowEnd),
		Resolution: durationpb.New(timeutil.Day),
	}
}

func opencostWindow(start time.Time, end time.Time) opencost.Window {
	return opencost.NewWindow(&start, &end)
}

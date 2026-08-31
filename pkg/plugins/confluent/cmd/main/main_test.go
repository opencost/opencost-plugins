package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	confluentplugin "github.com/opencost/opencost-plugins/pkg/plugins/confluent/confluentplugin"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestGetCustomCostsFetchesPagedCosts(t *testing.T) {
	var serverURL string
	var requestCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.URL.Path != "/billing/v1/costs" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "test-key" || pass != "test-secret" {
			t.Fatalf("unexpected basic auth: %q %q %t", user, pass, ok)
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Fatalf("unexpected accept header: %s", r.Header.Get("Accept"))
		}

		switch requestCount {
		case 1:
			if r.URL.Query().Get("start_date") != "2026-05-01" {
				t.Fatalf("unexpected start_date: %s", r.URL.Query().Get("start_date"))
			}
			if r.URL.Query().Get("end_date") != "2026-05-02" {
				t.Fatalf("unexpected end_date: %s", r.URL.Query().Get("end_date"))
			}
			if r.URL.Query().Get("page_size") != "5000" {
				t.Fatalf("unexpected page_size: %s", r.URL.Query().Get("page_size"))
			}
			_ = json.NewEncoder(w).Encode(costList{
				APIVersion: "billing/v1",
				Kind:       "CostList",
				Metadata: costListMetadata{
					Next: serverURL + "/billing/v1/costs?page_token=next-token",
				},
				Data: []costItem{
					{
						ID:                "cost-1",
						StartDate:         "2026-05-01",
						EndDate:           "2026-05-02",
						Granularity:       "DAILY",
						NetworkAccessType: "INTERNET",
						Product:           "KAFKA",
						LineType:          "KAFKA_NUM_CKUS",
						Price:             1.50,
						Unit:              "CKU",
						Quantity:          10,
						OriginalAmount:    15.00,
						DiscountAmount:    2.00,
						Amount:            13.00,
						Description:       "KAFKA101",
						Resource: &costResource{
							ID:          "lkc-123",
							DisplayName: "prod-kafka",
							Environment: &costEnvironment{
								ID: "env-123",
							},
						},
					},
				},
			})
		case 2:
			if r.URL.Query().Get("page_token") != "next-token" {
				t.Fatalf("unexpected page_token: %s", r.URL.Query().Get("page_token"))
			}
			_ = json.NewEncoder(w).Encode(costList{
				APIVersion: "billing/v1",
				Kind:       "CostList",
				Data: []costItem{
					{
						ID:             "credit-1",
						StartDate:      "2026-05-01",
						EndDate:        "2026-05-02",
						Product:        "KAFKA",
						LineType:       "PROMO_CREDIT",
						Unit:           "USD",
						Quantity:       1,
						OriginalAmount: -5,
						Amount:         -5,
					},
				},
			})
		default:
			t.Fatalf("unexpected request count: %d", requestCount)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	source := ConfluentCostSource{
		client: server.Client(),
		config: &confluentplugin.ConfluentConfig{
			APIKey:     "test-key",
			APISecret:  "test-secret",
			APIBaseURL: server.URL,
			PageSize:   confluentplugin.DefaultPageSize,
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

	first := resp[0].Costs[0]
	if first.BilledCost != 13.00 {
		t.Fatalf("unexpected billed cost: %f", first.BilledCost)
	}
	if first.ListCost != 15.00 {
		t.Fatalf("unexpected list cost: %f", first.ListCost)
	}
	if first.AccountName != "env-123" {
		t.Fatalf("unexpected account name: %s", first.AccountName)
	}
	if first.ResourceName != "prod-kafka" {
		t.Fatalf("unexpected resource name: %s", first.ResourceName)
	}
	if first.ResourceType != "KAFKA" {
		t.Fatalf("unexpected resource type: %s", first.ResourceType)
	}
	if first.ChargeCategory != "Usage" {
		t.Fatalf("unexpected charge category: %s", first.ChargeCategory)
	}
	if first.UsageQuantity != 10 {
		t.Fatalf("unexpected usage quantity: %f", first.UsageQuantity)
	}
	if first.UsageUnit != "CKU" {
		t.Fatalf("unexpected usage unit: %s", first.UsageUnit)
	}
	if first.ExtendedAttributes.GetProvider() != "Confluent" {
		t.Fatalf("unexpected provider: %s", first.ExtendedAttributes.GetProvider())
	}
	if first.ExtendedAttributes.GetSubAccountId() != "lkc-123" {
		t.Fatalf("unexpected sub account id: %s", first.ExtendedAttributes.GetSubAccountId())
	}

	second := resp[0].Costs[1]
	if second.ChargeCategory != "Credit" {
		t.Fatalf("unexpected credit charge category: %s", second.ChargeCategory)
	}
	if requestCount != 2 {
		t.Fatalf("expected two requests, got %d", requestCount)
	}
}

func TestGetCustomCostsRejectsHourlyResolution(t *testing.T) {
	source := ConfluentCostSource{
		client: http.DefaultClient,
		config: &confluentplugin.ConfluentConfig{},
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

func TestGetCostsRejectsUnexpectedPaginationHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(costList{
			Metadata: costListMetadata{
				Next: "https://example.com/billing/v1/costs?page_token=bad",
			},
		})
	}))
	defer server.Close()

	source := ConfluentCostSource{
		client: server.Client(),
		config: &confluentplugin.ConfluentConfig{
			APIKey:     "test-key",
			APISecret:  "test-secret",
			APIBaseURL: server.URL,
			PageSize:   confluentplugin.DefaultPageSize,
		},
	}

	resp := source.GetCustomCosts(customCostRequest())
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

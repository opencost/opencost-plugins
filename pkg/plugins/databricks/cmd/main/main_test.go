package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	databricksplugin "github.com/opencost/opencost-plugins/pkg/plugins/databricks/databricksplugin"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestGetCustomCostsFetchesMonthlyCSVAndFiltersDailyWindows(t *testing.T) {
	var requestCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.URL.Path != "/api/2.0/accounts/abc-123/usage/download" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept") != "text/plain" {
			t.Fatalf("unexpected accept header: %s", r.Header.Get("Accept"))
		}
		if r.URL.Query().Get("start_month") != "2026-05" || r.URL.Query().Get("end_month") != "2026-05" {
			t.Fatalf("unexpected month query: %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("personal_data") != "true" {
			t.Fatalf("expected personal_data query, got %s", r.URL.RawQuery)
		}

		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Join([]string{
			"record_id,account_id,workspace_id,sku_name,cloud,usage_start_time,usage_end_time,usage_date,usage_unit,usage_quantity,billing_origin_product,usage_type,record_type,usage_metadata.cluster_id",
			"rec-1,abc-123,ws-1,STANDARD_ALL_PURPOSE_COMPUTE,AWS,2026-05-01 10:00:00.000+00:00,2026-05-01 11:00:00.000+00:00,2026-05-01,DBU,2.5,ALL_PURPOSE,COMPUTE_TIME,ORIGINAL,cluster-1",
			"rec-2,abc-123,ws-1,STANDARD_ALL_PURPOSE_COMPUTE,AWS,2026-05-02 10:00:00.000+00:00,2026-05-02 11:00:00.000+00:00,2026-05-02,DBU,3.0,ALL_PURPOSE,COMPUTE_TIME,ORIGINAL,cluster-1",
			"",
		}, "\n")))
	}))
	defer server.Close()

	source := DatabricksCostSource{
		client: server.Client(),
		config: &databricksplugin.DatabricksConfig{
			AccountID:           "abc-123",
			Token:               "test-token",
			AccountAPIBaseURL:   server.URL,
			IncludePersonalData: true,
			SKUUnitPrices: map[string]float64{
				"STANDARD_ALL_PURPOSE_COMPUTE": 0.55,
			},
		},
	}

	resp := source.GetCustomCosts(customCostRequest())
	if len(resp) != 2 {
		t.Fatalf("expected two responses, got %d", len(resp))
	}
	if requestCount != 1 {
		t.Fatalf("expected monthly API response to be cached, got %d requests", requestCount)
	}
	if len(resp[0].Errors) != 0 {
		t.Fatalf("unexpected errors: %v", resp[0].Errors)
	}
	if len(resp[0].Costs) != 1 {
		t.Fatalf("expected one first-day custom cost, got %d", len(resp[0].Costs))
	}
	if len(resp[1].Costs) != 1 {
		t.Fatalf("expected one second-day custom cost, got %d", len(resp[1].Costs))
	}

	first := resp[0].Costs[0]
	if first.BilledCost != 1.375 {
		t.Fatalf("unexpected billed cost: %f", first.BilledCost)
	}
	if first.AccountName != "abc-123" {
		t.Fatalf("unexpected account name: %s", first.AccountName)
	}
	if first.ResourceName != "cluster-1" {
		t.Fatalf("unexpected resource name: %s", first.ResourceName)
	}
	if first.ResourceType != "ALL_PURPOSE" {
		t.Fatalf("unexpected resource type: %s", first.ResourceType)
	}
	if first.UsageQuantity != 2.5 {
		t.Fatalf("unexpected usage quantity: %f", first.UsageQuantity)
	}
	if first.ExtendedAttributes.GetSkuId() != "STANDARD_ALL_PURPOSE_COMPUTE" {
		t.Fatalf("unexpected sku id: %s", first.ExtendedAttributes.GetSkuId())
	}
}

func TestGetCustomCostsRejectsHourlyResolution(t *testing.T) {
	source := DatabricksCostSource{
		client: http.DefaultClient,
		config: &databricksplugin.DatabricksConfig{},
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

func TestParseUsageCSVSupportsLegacyHeaders(t *testing.T) {
	csvBody := strings.NewReader(strings.Join([]string{
		"recordId,workspaceId,sku,timestamp,dbus,unit,product,clusterId",
		"legacy-1,ws-1,SKU_A,2026-05-01 00:00:00,4.5,DBU,JOBS,cluster-1",
		"",
	}, "\n"))

	records, err := parseUsageCSV(csvBody)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one record, got %d", len(records))
	}
	if records[0].RecordID != "legacy-1" || records[0].WorkspaceID != "ws-1" || records[0].SKUName != "SKU_A" {
		t.Fatalf("unexpected parsed record: %+v", records[0])
	}
	if records[0].UsageQuantity != 4.5 {
		t.Fatalf("unexpected usage quantity: %f", records[0].UsageQuantity)
	}
}

// TestGetCustomCostsRecoversFromExpiredToken reproduces what a real
// Databricks-on-GCP account returns for an expired token (403 with the reason
// only in WWW-Authenticate) and checks that the plugin fetches a new token
// and retries instead of failing every window.
func TestGetCustomCostsRecoversFromExpiredToken(t *testing.T) {
	var mu sync.Mutex
	tokenRequests, usageRequests := 0, 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/oidc/accounts/abc-123/v1/token":
			tokenRequests++
			user, pass, _ := r.BasicAuth()
			if user != "client-1" || pass != "secret-1" {
				t.Errorf("unexpected client credentials %q/%q", user, pass)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"access_token":"token-%d","token_type":"Bearer","expires_in":3600}`, tokenRequests)
		case "/api/2.0/accounts/abc-123/usage/download":
			usageRequests++
			if r.Header.Get("Authorization") == "Bearer token-1" {
				// The first token is rejected as expired, like Databricks does.
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", error_description="The access token expired"`)
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("Invalid Token"))
				return
			}
			if r.Header.Get("Authorization") != "Bearer token-2" {
				t.Errorf("unexpected auth header %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte("workspaceId,timestamp,clusterId,sku,dbus\n" +
				"111,2026-05-01T09:59:59.999Z,c-1,PREMIUM_JOBS_COMPUTE,2\n"))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	config := &databricksplugin.DatabricksConfig{
		AccountID:         "abc-123",
		ClientID:          "client-1",
		ClientSecret:      "secret-1",
		Scope:             "billing:read",
		AccountAPIBaseURL: server.URL,
		DefaultUnitPrice:  0.5,
	}
	tokens, err := databricksplugin.NewTokenProvider(config, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	source := DatabricksCostSource{client: server.Client(), config: config, tokens: tokens}

	resp := source.GetCustomCosts(customCostRequest())
	if len(resp) != 2 {
		t.Fatalf("expected two responses, got %d", len(resp))
	}
	for _, r := range resp {
		if len(r.Errors) != 0 {
			t.Fatalf("unexpected errors: %v", r.Errors)
		}
	}
	if len(resp[0].Costs) != 1 || resp[0].Costs[0].BilledCost != 1 {
		t.Fatalf("expected one $1 cost on day one, got %+v", resp[0].Costs)
	}
	if tokenRequests != 2 || usageRequests != 2 {
		t.Fatalf("expected 2 token and 2 usage requests, got %d and %d", tokenRequests, usageRequests)
	}
}

func TestExpiredStaticTokenReportsReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", error_description="The access token expired"`)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("Invalid Token"))
	}))
	defer server.Close()

	config := &databricksplugin.DatabricksConfig{AccountID: "abc-123", Token: "old", AccountAPIBaseURL: server.URL}
	tokens, _ := databricksplugin.NewTokenProvider(config, server.Client())
	source := DatabricksCostSource{client: server.Client(), config: config, tokens: tokens}

	resp := source.GetCustomCosts(customCostRequest())
	if len(resp[0].Errors) != 1 || !strings.Contains(resp[0].Errors[0], "The access token expired") {
		t.Fatalf("expected the expiry reason in the error, got %v", resp[0].Errors)
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

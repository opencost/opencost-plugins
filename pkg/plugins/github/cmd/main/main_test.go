package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	githubplugin "github.com/opencost/opencost-plugins/pkg/plugins/github/githubplugin"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestGetCustomCostsForOrganization(t *testing.T) {
	var sawPath string
	var sawAuth string
	var sawVersion string
	var sawQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		sawAuth = r.Header.Get("Authorization")
		sawVersion = r.Header.Get("X-GitHub-Api-Version")
		sawQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(usageReport{
			UsageItems: []usageItem{
				{
					Date:             "2026-05-01",
					Product:          "Actions",
					SKU:              "actions_linux",
					Quantity:         100,
					UnitType:         "minutes",
					PricePerUnit:     0.008,
					GrossAmount:      0.80,
					DiscountAmount:   0.10,
					NetAmount:        0.70,
					OrganizationName: "opencost",
					RepositoryName:   "opencost/opencost",
				},
				{
					Date:             "2026-05-01",
					Product:          "Packages",
					SKU:              "packages_storage",
					Quantity:         2.5,
					UnitType:         "gb-months",
					PricePerUnit:     0.25,
					GrossAmount:      0.625,
					DiscountAmount:   0,
					NetAmount:        0.625,
					OrganizationName: "opencost",
				},
			},
		})
	}))
	defer server.Close()

	source := GitHubCostSource{
		client: server.Client(),
		config: &githubplugin.GitHubConfig{
			Token:       "test-token",
			Account:     "opencost",
			AccountType: githubplugin.AccountTypeOrg,
			APIBaseURL:  server.URL,
			APIVersion:  "2022-11-28",
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
	if first.ResourceName != "actions_linux" {
		t.Fatalf("unexpected resource name: %s", first.ResourceName)
	}
	if first.ResourceType != "Actions" {
		t.Fatalf("unexpected resource type: %s", first.ResourceType)
	}
	if first.BilledCost != 0.70 {
		t.Fatalf("unexpected billed cost: %f", first.BilledCost)
	}
	if first.ListCost != 0.80 {
		t.Fatalf("unexpected list cost: %f", first.ListCost)
	}
	if first.UsageQuantity != 100 {
		t.Fatalf("unexpected usage quantity: %f", first.UsageQuantity)
	}
	if first.UsageUnit != "minutes" {
		t.Fatalf("unexpected usage unit: %s", first.UsageUnit)
	}

	if sawPath != "/organizations/opencost/settings/billing/usage" {
		t.Fatalf("unexpected request path: %s", sawPath)
	}
	if sawAuth != "Bearer test-token" {
		t.Fatalf("unexpected auth header: %s", sawAuth)
	}
	if sawVersion != "2022-11-28" {
		t.Fatalf("unexpected api version: %s", sawVersion)
	}
	if sawQuery != "day=1&month=5&year=2026" {
		t.Fatalf("unexpected query: %s", sawQuery)
	}
}

func TestGetCustomCostsForUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/monalisa/settings/billing/usage" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(usageReport{})
	}))
	defer server.Close()

	source := GitHubCostSource{
		client: server.Client(),
		config: &githubplugin.GitHubConfig{
			Token:       "test-token",
			Account:     "monalisa",
			AccountType: githubplugin.AccountTypeUser,
			APIBaseURL:  server.URL,
			APIVersion:  "2022-11-28",
		},
	}

	resp := source.GetCustomCosts(customCostRequest())
	if len(resp) != 1 {
		t.Fatalf("expected one response, got %d", len(resp))
	}
	if len(resp[0].Errors) != 0 {
		t.Fatalf("unexpected errors: %v", resp[0].Errors)
	}
}

func TestGetCustomCostsRejectsHourlyResolution(t *testing.T) {
	source := GitHubCostSource{
		client: http.DefaultClient,
		config: &githubplugin.GitHubConfig{},
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

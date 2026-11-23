package main

import (
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	databricksplugin "github.com/opencost/opencost-plugins/pkg/plugins/databricks/databricksplugin"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// testdata/gcp_serverless_usage.csv is a real response from
// GET /api/2.0/accounts/{id}/usage/download on a Databricks-on-GCP trial
// (serverless SQL warehouse, notebook, job, pipeline and AI usage), with
// all IDs replaced by placeholders of the same shape.
func realGCPCosts(t *testing.T, config *databricksplugin.DatabricksConfig) []*pb.CustomCost {
	t.Helper()
	body, err := os.ReadFile("testdata/gcp_serverless_usage.csv")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	config.AccountID = "abc-123"
	config.Token = "test-token"
	config.AccountAPIBaseURL = server.URL
	source := DatabricksCostSource{client: server.Client(), config: config}

	day := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	resp := source.GetCustomCosts(&pb.CustomCostRequest{
		Start:      timestamppb.New(day),
		End:        timestamppb.New(day.Add(timeutil.Day)),
		Resolution: durationpb.New(timeutil.Day),
	})
	if len(resp) != 1 || len(resp[0].Errors) != 0 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	return resp[0].Costs
}

func TestRealGCPUsageTotalsPerSKU(t *testing.T) {
	costs := realGCPCosts(t, &databricksplugin.DatabricksConfig{})
	if len(costs) != 19 {
		t.Fatalf("expected 19 costs, got %d", len(costs))
	}

	want := map[string]float64{
		"PREMIUM_SERVERLESS_SQL_COMPUTE_US_IOWA":         4.6230,
		"PREMIUM_JOBS_SERVERLESS_COMPUTE_US_IOWA":        1.6935,
		"PREMIUM_ALL_PURPOSE_SERVERLESS_COMPUTE_US_IOWA": 0.7159,
		"PREMIUM_SERVERLESS_REAL_TIME_INFERENCE_US_IOWA": 0.1204,
	}
	got := map[string]float64{}
	for _, c := range costs {
		got[c.ExtendedAttributes.GetSkuId()] += float64(c.UsageQuantity)
	}
	for sku, dbus := range want {
		if math.Abs(got[sku]-dbus) > 0.001 {
			t.Errorf("%s: %.4f DBUs, want %.4f", sku, got[sku], dbus)
		}
	}
}

func TestRealGCPUsageHasUniqueStableProviderIDs(t *testing.T) {
	first := realGCPCosts(t, &databricksplugin.DatabricksConfig{})
	second := realGCPCosts(t, &databricksplugin.DatabricksConfig{})

	seen := map[string]bool{}
	for i, c := range first {
		if seen[c.ProviderId] {
			t.Errorf("duplicate provider ID %q", c.ProviderId)
		}
		seen[c.ProviderId] = true
		if c.ProviderId != second[i].ProviderId {
			t.Errorf("provider ID not stable across downloads: %q vs %q", c.ProviderId, second[i].ProviderId)
		}
	}
}

func TestRealGCPUsageResourceNamesComeFromTags(t *testing.T) {
	costs := realGCPCosts(t, &databricksplugin.DatabricksConfig{})

	counts := map[string]int{}
	for _, c := range costs {
		counts[c.ResourceName]++
	}
	want := map[string]int{
		"sql-warehouse/0a1b2c3d4e5f6a7b":                              4,
		"model-serving/1234567890123456":                              6,
		"model/gpt-oss":                                               1,
		"notebook/3333333333333333":                                   2,
		"job/444444444444444":                                         1,
		"serverless-jobs/1234567890123456":                            2,
		"pipeline/dlt-execution-00000000-1111-2222-3333-444444444444": 1,
		"1122-000000-abcd1234-v2n":                                    1,
		"1234567890123456":                                            1, // all-purpose row with empty tags
	}
	for name, n := range want {
		if counts[name] != n {
			t.Errorf("resource %q: %d rows, want %d (all: %v)", name, counts[name], n, counts)
		}
	}
}

func TestRealGCPUsageUnitPricesAndMetadata(t *testing.T) {
	costs := realGCPCosts(t, &databricksplugin.DatabricksConfig{
		SKUUnitPrices: map[string]float64{
			// GCP SKU names carry a region suffix; keys must match exactly.
			"PREMIUM_SERVERLESS_SQL_COMPUTE_US_IOWA": 0.70,
		},
		DefaultUnitPrice: 0.50,
	})

	var sqlCost, otherCost float64
	for _, c := range costs {
		if c.UsageUnit != "DBU" || c.ExtendedAttributes.GetPricingUnit() != "DBU" {
			t.Fatalf("expected DBU unit, got %q / %q", c.UsageUnit, c.ExtendedAttributes.GetPricingUnit())
		}
		if c.ExtendedAttributes.GetSkuId() == "PREMIUM_SERVERLESS_SQL_COMPUTE_US_IOWA" {
			sqlCost += float64(c.BilledCost)
			if c.ListUnitPrice != 0.70 {
				t.Fatalf("list unit price %v, want 0.70", c.ListUnitPrice)
			}
		} else {
			otherCost += float64(c.BilledCost)
		}
	}
	if math.Abs(sqlCost-4.6230*0.70) > 0.001 {
		t.Errorf("SQL cost %.4f, want %.4f", sqlCost, 4.6230*0.70)
	}
	if math.Abs(otherCost-(1.6935+0.7159+0.1204)*0.50) > 0.001 {
		t.Errorf("other cost %.4f", otherCost)
	}

	for _, c := range costs {
		if c.ResourceName == "job/444444444444444" {
			if c.Metadata["RunName"] != "NewJob2026-11-22000000" || c.Metadata["DatabricksScgwWorkloadType"] != "Jobs" {
				t.Fatalf("expected job tags in metadata, got %v", c.Metadata)
			}
			return
		}
	}
	t.Fatal("job row not found")
}

func TestParseTags(t *testing.T) {
	if parseTags("") != nil || parseTags("{}") != nil || parseTags("not json") != nil {
		t.Fatal("expected nil for empty or invalid tags")
	}
	tags := parseTags(`{"team":"data","count":3,"on":true}`)
	if tags["team"] != "data" || tags["count"] != "3" || tags["on"] != "true" {
		t.Fatalf("unexpected tags %v", tags)
	}
}

func TestRowKeyDistinguishesIdenticalRows(t *testing.T) {
	seen := map[string]int{}
	row := []string{"ws", "2026-11-22T13:59:59.000Z", "", "SKU", "1"}
	a, b := rowKey(row, seen), rowKey(row, seen)
	if a == b {
		t.Fatalf("identical rows got the same key %q", a)
	}
	if c := rowKey([]string{"ws", "2026-11-22T13:59:59.000Z", "", "SKU", "2"}, seen); c == a || c == b {
		t.Fatalf("different rows collided")
	}
}

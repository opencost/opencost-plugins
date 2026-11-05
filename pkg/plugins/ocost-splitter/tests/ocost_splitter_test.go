package tests

import (
	"encoding/json"
	"net/http"
	"os"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencost/opencost-plugins/pkg/plugins/ocost-splitter/ocostsplitterplugin"
)

// stubOpenCost returns an OpenCost allocation API server with a fixed estate:
// three shared-infra namespaces carrying the shared pool, and three attributed
// namespaces with unequal costs.
func stubOpenCost(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/allocation") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		resp := map[string]interface{}{
			"code": 200,
			"data": []map[string]interface{}{
				{"name": "kube-system", "totalCost": 100.0},
				{"name": "monitoring", "totalCost": 60.0},
				{"name": "ingress-nginx", "totalCost": 40.0},
				{"name": "team-alpha", "totalCost": 300.0},
				{"name": "team-beta", "totalCost": 200.0},
				{"name": "team-gamma", "totalCost": 100.0},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func makeConfig(api string, method string) ocostsplitterplugin.Config {
	return ocostsplitterplugin.Config{
		OpencostAPIAddress: api,
		SplitMethod:        method,
		SharedNamespaces:   []string{"kube-system", "monitoring", "ingress-nginx"},
		Currency:           "USD",
	}
}

func TestProportionalSplit(t *testing.T) {
	srv := stubOpenCost(t)
	defer srv.Close()

	// Shared pool = 100 + 60 + 40 = 200.
	// Attributed total = 300 + 200 + 100 = 600.
	// Proportional shares: alpha 100, beta 66.67, gamma 33.33.
	cfg := makeConfig(srv.URL, ocostsplitterplugin.MethodProportional)
	allocs, err := ocostsplitterplugin.SplitShared(200, []ocostsplitterplugin.Target{
		{Namespace: "team-alpha", Weight: 300},
		{Namespace: "team-beta", Weight: 200},
		{Namespace: "team-gamma", Weight: 100},
	}, ocostsplitterplugin.MethodProportional)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if diff := allocs["team-alpha"] - 100.0; diff > 0.01 || diff < -0.01 {
		t.Fatalf("expected team-alpha ~100, got %f", allocs["team-alpha"])
	}
	if diff := allocs["team-beta"] - 66.67; diff > 0.01 || diff < -0.01 {
		t.Fatalf("expected team-beta ~66.67, got %f", allocs["team-beta"])
	}
	if diff := allocs["team-gamma"] - 33.33; diff > 0.01 || diff < -0.01 {
		t.Fatalf("expected team-gamma ~33.33, got %f", allocs["team-gamma"])
	}
	_ = cfg
}

func TestEqualSplit(t *testing.T) {
	allocs, err := ocostsplitterplugin.SplitShared(200, []ocostsplitterplugin.Target{
		{Namespace: "team-alpha", Weight: 300},
		{Namespace: "team-beta", Weight: 200},
		{Namespace: "team-gamma", Weight: 100},
	}, ocostsplitterplugin.MethodEqual)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if diff := allocs["team-alpha"] - 66.67; diff > 0.01 || diff < -0.01 {
		t.Fatalf("expected ~66.67 each, got %f", allocs["team-alpha"])
	}
}

func TestWeightedSplit(t *testing.T) {
	dir := t.TempDir()
	weightsFile := dir + "/weights.csv"
	if err := writeWeights(weightsFile, "namespace,weight\nteam-alpha,5\nteam-beta,3\nteam-gamma,2\n"); err != nil {
		t.Fatal(err)
	}
	weights, err := ocostsplitterplugin.LoadWeights(weightsFile)
	if err != nil {
		t.Fatal(err)
	}
	targets := []ocostsplitterplugin.Target{
		{Namespace: "team-alpha", Weight: weights["team-alpha"]},
		{Namespace: "team-beta", Weight: weights["team-beta"]},
		{Namespace: "team-gamma", Weight: weights["team-gamma"]},
	}
	allocs, err := ocostsplitterplugin.SplitShared(200, targets, ocostsplitterplugin.MethodWeighted)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if diff := allocs["team-alpha"] - 100.0; diff > 0.01 || diff < -0.01 {
		t.Fatalf("expected team-alpha 100 (5/10 of 200), got %f", allocs["team-alpha"])
	}
}

func writeWeights(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

func TestNoSplitBaseline(t *testing.T) {
	allocs, err := ocostsplitterplugin.SplitShared(200, []ocostsplitterplugin.Target{
		{Namespace: "team-alpha", Weight: 300},
	}, ocostsplitterplugin.MethodNone)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if allocs["shared"] != 200 {
		t.Fatalf("expected unsplit pool of 200 on shared, got %f", allocs["shared"])
	}
}

func TestNegativePoolRejected(t *testing.T) {
	_, err := ocostsplitterplugin.SplitShared(-1, []ocostsplitterplugin.Target{
		{Namespace: "a", Weight: 1},
	}, ocostsplitterplugin.MethodEqual)
	if err == nil {
		t.Fatal("negative pool should be rejected")
	}
}

func TestZeroWeightTotalRejected(t *testing.T) {
	_, err := ocostsplitterplugin.SplitShared(200, []ocostsplitterplugin.Target{
		{Namespace: "a", Weight: 0},
	}, ocostsplitterplugin.MethodProportional)
	if err == nil {
		t.Fatal("zero total weight should be rejected for proportional")
	}
}

func TestUnknownMethodRejected(t *testing.T) {
	_, err := ocostsplitterplugin.SplitShared(200, []ocostsplitterplugin.Target{
		{Namespace: "a", Weight: 1},
	}, "mood-based")
	if err == nil {
		t.Fatal("unknown method should be rejected")
	}
}

func TestEmptyTargetsRejected(t *testing.T) {
	_, err := ocostsplitterplugin.SplitShared(200, nil, ocostsplitterplugin.MethodEqual)
	if err == nil {
		t.Fatal("empty targets should be rejected")
	}
}

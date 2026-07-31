package datadog

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func doRoundTrip(t *testing.T, path, rawQuery string) string {
	t.Helper()

	var gotMonth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMonth = r.URL.Query().Get("month")
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+path+"?"+rawQuery, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}

	client := &http.Client{Transport: MonthParamRoundTripper{}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	resp.Body.Close()

	return gotMonth
}

func TestMonthParamRewrittenOnBillableSummary(t *testing.T) {
	got := doRoundTrip(t, "/api/v1/usage/billable-summary", "month=2024-03-01T00%3A00%3A00Z")
	if got != "2024-03" {
		t.Fatalf("expected month=2024-03, got month=%s", got)
	}
}

func TestMonthAlreadyShortFormPassedThrough(t *testing.T) {
	// If the client library starts emitting YYYY-MM itself, the rewrite must
	// be a no-op rather than corrupting the value.
	got := doRoundTrip(t, "/api/v1/usage/billable-summary", "month=2024-03")
	if got != "2024-03" {
		t.Fatalf("expected month=2024-03, got month=%s", got)
	}
}

func TestOtherEndpointsUntouched(t *testing.T) {
	got := doRoundTrip(t, "/api/v2/usage/hourly_usage", "month=2024-03-01T00%3A00%3A00Z")
	if got != "2024-03-01T00:00:00Z" {
		t.Fatalf("expected month unchanged, got month=%s", got)
	}
}

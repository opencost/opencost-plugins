package datadog

import (
	"net/http"
	"strings"
	"time"
)

// billableSummaryPath is the API path whose `month` query parameter requires
// YYYY-MM formatting.
const billableSummaryPath = "/api/v1/usage/billable-summary"

// MonthParamRoundTripper rewrites the `month` query parameter on
// /api/v1/usage/billable-summary requests from the RFC3339 timestamp the
// generated datadog-api-client-go emits (e.g. 2024-03-01T00:00:00Z) to the
// YYYY-MM format the Datadog API requires. Without this rewrite the API
// rejects every request with:
//
//	400 Bad Request: {"errors":[{"status":"400","title":"Bad Request",
//	  "detail":"month must be in the format YYYY-MM"}]}
//
// which causes GetDDUnitPrices, and therefore every GetCustomCosts window,
// to fail.
type MonthParamRoundTripper struct {
	// Next is the underlying RoundTripper to delegate to. If nil,
	// http.DefaultTransport is used.
	Next http.RoundTripper
}

func (m MonthParamRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, billableSummaryPath) {
		q := req.URL.Query()
		if month := q.Get("month"); month != "" {
			if t, err := time.Parse(time.RFC3339, month); err == nil {
				q.Set("month", t.UTC().Format("2006-01"))
				req.URL.RawQuery = q.Encode()
			}
		}
	}
	next := m.Next
	if next == nil {
		next = http.DefaultTransport
	}
	return next.RoundTrip(req)
}

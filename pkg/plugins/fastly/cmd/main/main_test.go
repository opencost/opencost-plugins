package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/opencost/opencost-plugins/pkg/plugins/fastly/fastlyplugin"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MockHTTPClient implements HTTPClient interface for testing
type MockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return nil, fmt.Errorf("DoFunc not implemented")
}

// createMockResponse creates a mock HTTP response with given status code and body
func createMockResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// Test request validation
func TestValidateRequest(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		req            *pb.CustomCostRequest
		expectedErrors int
		errorContains  []string
	}{
		{
			name: "Valid request",
			req: &pb.CustomCostRequest{
				Start:      timestamppb.New(now.AddDate(0, -1, 0)),
				End:        timestamppb.New(now),
				Resolution: durationpb.New(24 * time.Hour),
			},
			expectedErrors: 0,
			errorContains:  []string{},
		},
		{
			name: "Resolution less than an hour",
			req: &pb.CustomCostRequest{
				Start:      timestamppb.New(now.AddDate(0, -1, 0)),
				End:        timestamppb.New(now),
				Resolution: durationpb.New(30 * time.Minute),
			},
			expectedErrors: 1,
			errorContains:  []string{"Resolution should be at least one hour"},
		},
		{
			name: "Start date more than 12 months ago",
			req: &pb.CustomCostRequest{
				Start:      timestamppb.New(now.AddDate(0, -13, 0)),
				End:        timestamppb.New(now),
				Resolution: durationpb.New(24 * time.Hour),
			},
			expectedErrors: 1,
			errorContains:  []string{"more than 12 months in the past"},
		},
		{
			name: "End date before start date",
			req: &pb.CustomCostRequest{
				Start:      timestamppb.New(now),
				End:        timestamppb.New(now.AddDate(0, -1, 0)),
				Resolution: durationpb.New(24 * time.Hour),
			},
			expectedErrors: 1,
			errorContains:  []string{"End date cannot be before start date"},
		},
		{
			name: "Future dates requested",
			req: &pb.CustomCostRequest{
				Start:      timestamppb.New(now.AddDate(0, 1, 0)),
				End:        timestamppb.New(now.AddDate(0, 2, 0)),
				Resolution: durationpb.New(24 * time.Hour),
			},
			expectedErrors: 0, // Future dates now return empty response instead of error
			errorContains:  []string{},
		},
		{
			name: "Multiple validation errors",
			req: &pb.CustomCostRequest{
				Start:      timestamppb.New(now.AddDate(0, -13, 0)), // Too far in past
				End:        timestamppb.New(now.AddDate(0, -14, 0)), // Before start
				Resolution: durationpb.New(30 * time.Minute),        // Too small
			},
			expectedErrors: 3,
			errorContains: []string{
				"Resolution should be at least one hour",
				"more than 12 months in the past",
				"End date cannot be before start date",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := validateRequest(tt.req)

			if len(errors) != tt.expectedErrors {
				t.Errorf("Expected %d errors, got %d. Errors: %v",
					tt.expectedErrors, len(errors), errors)
			}

			for _, expectedContains := range tt.errorContains {
				found := false
				for _, err := range errors {
					if strings.Contains(err, expectedContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error containing '%s' not found in %v",
						expectedContains, errors)
				}
			}
		})
	}
}

// Config tests have been moved to config/fastlyconfig_test.go

func TestGetMonthToDateInvoice(t *testing.T) {
	// Arrange
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Verify the request
			if req.Method != "GET" {
				t.Errorf("Expected GET request, got %s", req.Method)
			}
			if !strings.Contains(req.URL.Path, "/billing/v3/invoices/month-to-date") {
				t.Errorf("Expected month-to-date endpoint, got %s", req.URL.Path)
			}
			if req.Header.Get("Fastly-Key") != "test-api-key" {
				t.Errorf("Expected API key in header")
			}

			return createMockResponse(200, `{
                "customer_id": "test-customer",
                "invoice_id": "mtd-12345",
                "billing_start_date": "2024-01-01T00:00:00Z",
                "billing_end_date": "2024-01-31T00:00:00Z",
                "currency_code": "USD",
                "monthly_transaction_amount": 50.75,
                "transaction_line_items": [
                    {
                        "description": "CDN Bandwidth",
                        "amount": 50.75,
                        "rate": 0.05,
                        "units": 1015,
                        "product_name": "CDN",
                        "product_group": "Full Site Delivery",
                        "product_line": "Network Services",
                        "region": "Global",
                        "usage_type": "bandwidth"
                    }
                ]
            }`), nil
		},
	}

	rateLimiter := rate.NewLimiter(rate.Every(time.Second), 10)
	fastlyCostSrc := FastlyCostSource{
		apiKey:       "test-api-key",
		httpClient:   mockClient,
		rateLimiter:  rateLimiter,
		invoiceCache: make(map[string][]fastlyplugin.Invoice),
	}

	// Act
	invoice, err := fastlyCostSrc.getMonthToDateInvoice()

	// Assert
	if err != nil {
		t.Fatalf("Failed to get month-to-date invoice: %v", err)
	}
	if invoice == nil {
		t.Fatal("Received nil invoice")
	}
	if invoice.InvoiceID != "mtd-12345" {
		t.Errorf("Expected invoice ID mtd-12345, got %s", invoice.InvoiceID)
	}
	if len(invoice.TransactionLineItems) != 1 {
		t.Errorf("Expected 1 transaction line item, got %d", len(invoice.TransactionLineItems))
	}
}

func TestGetMonthToDateInvoiceError(t *testing.T) {
	// Arrange
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return createMockResponse(403, `{
                "msg": "Unauthorized",
                "detail": "Invalid API key"
            }`), nil
		},
	}

	rateLimiter := rate.NewLimiter(rate.Every(time.Second), 10)
	fastlyCostSrc := FastlyCostSource{
		apiKey:       "invalid-api-key",
		httpClient:   mockClient,
		rateLimiter:  rateLimiter,
		invoiceCache: make(map[string][]fastlyplugin.Invoice),
	}

	// Act
	invoice, err := fastlyCostSrc.getMonthToDateInvoice()

	// Assert
	if err == nil {
		t.Error("Expected error for unauthorized request, got nil")
	}
	if invoice != nil {
		t.Error("Expected nil invoice for error response")
	}
}

func TestGetInvoicesForPeriod(t *testing.T) {
	// Arrange
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Verify request parameters
			query := req.URL.Query()
			if query.Get("billing_start_date") != "2024-01-01" {
				t.Errorf("Expected billing_start_date=2024-01-01, got %s", query.Get("billing_start_date"))
			}
			if query.Get("billing_end_date") != "2024-01-31" {
				t.Errorf("Expected billing_end_date=2024-01-31, got %s", query.Get("billing_end_date"))
			}

			return createMockResponse(200, `{
                "data": [
                    {
                        "customer_id": "test-customer",
                        "invoice_id": "inv-12345",
                        "billing_start_date": "2024-01-01T00:00:00Z",
                        "billing_end_date": "2024-01-31T00:00:00Z",
                        "currency_code": "USD",
                        "monthly_transaction_amount": 100.50,
                        "transaction_line_items": [
                            {
                                "description": "CDN Bandwidth",
                                "amount": 100.50,
                                "rate": 0.05,
                                "units": 2010,
                                "product_name": "CDN",
                                "product_group": "Full Site Delivery",
                                "product_line": "Network Services",
                                "region": "Global",
                                "usage_type": "bandwidth"
                            }
                        ]
                    }
                ],
                "meta": {
                    "next_cursor": "",
                    "limit": 200,
                    "total": 1,
                    "sort": "billing_start_date"
                }
            }`), nil
		},
	}

	rateLimiter := rate.NewLimiter(rate.Every(time.Second), 10)
	fastlyCostSrc := FastlyCostSource{
		apiKey:       "test-api-key",
		httpClient:   mockClient,
		rateLimiter:  rateLimiter,
		invoiceCache: make(map[string][]fastlyplugin.Invoice),
	}

	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	// Act
	invoices, err := fastlyCostSrc.getInvoicesForPeriod(&startDate, &endDate)

	// Assert
	if err != nil {
		t.Fatalf("Failed to get invoices for period: %v", err)
	}
	if len(invoices) != 1 {
		t.Errorf("Expected 1 invoice, got %d", len(invoices))
	}
	if invoices[0].InvoiceID != "inv-12345" {
		t.Errorf("Expected invoice ID inv-12345, got %s", invoices[0].InvoiceID)
	}
}

func TestGetInvoicesForPeriodWithPagination(t *testing.T) {
	// Arrange
	callCount := 0
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			callCount++
			query := req.URL.Query()

			if callCount == 1 {
				// First page
				if query.Get("cursor") != "" {
					t.Errorf("Expected no cursor on first call")
				}
				return createMockResponse(200, `{
                    "data": [
                        {
                            "customer_id": "test-customer",
                            "invoice_id": "inv-12345",
                            "billing_start_date": "2024-01-01T00:00:00Z",
                            "billing_end_date": "2024-01-31T00:00:00Z",
                            "transaction_line_items": [
                                {
                                    "description": "CDN Bandwidth",
                                    "amount": 100.50,
                                    "rate": 0.05,
                                    "units": 2010,
                                    "product_name": "CDN",
                                    "product_group": "Full Site Delivery",
                                    "usage_type": "bandwidth"
                                }
                            ]
                        }
                    ],
                    "meta": {
                        "next_cursor": "page2",
                        "limit": 200,
                        "total": 2
                    }
                }`), nil
			} else if callCount == 2 {
				// Second page
				if query.Get("cursor") != "page2" {
					t.Errorf("Expected cursor=page2, got %s", query.Get("cursor"))
				}
				return createMockResponse(200, `{
                    "data": [
                        {
                            "customer_id": "test-customer",
                            "invoice_id": "inv-67890",
                            "billing_start_date": "2024-01-01T00:00:00Z",
                            "billing_end_date": "2024-01-31T00:00:00Z",
                            "transaction_line_items": [
                                {
                                    "description": "Compute Requests",
                                    "amount": 75.25,
                                    "rate": 0.001,
                                    "units": 75250,
                                    "product_name": "Compute",
                                    "product_group": "Compute",
                                    "usage_type": "requests"
                                }
                            ]
                        }
                    ],
                    "meta": {
                        "next_cursor": "",
                        "limit": 200,
                        "total": 2
                    }
                }`), nil
			}
			return nil, fmt.Errorf("unexpected call count: %d", callCount)
		},
	}

	rateLimiter := rate.NewLimiter(rate.Every(time.Second), 10)
	fastlyCostSrc := FastlyCostSource{
		apiKey:       "test-api-key",
		httpClient:   mockClient,
		rateLimiter:  rateLimiter,
		invoiceCache: make(map[string][]fastlyplugin.Invoice),
	}

	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	// Act
	invoices, err := fastlyCostSrc.getInvoicesForPeriod(&startDate, &endDate)

	// Assert
	if err != nil {
		t.Fatalf("Failed to get invoices for period: %v", err)
	}
	if len(invoices) != 2 {
		t.Errorf("Expected 2 invoices from pagination, got %d", len(invoices))
	}
	if callCount != 2 {
		t.Errorf("Expected 2 API calls for pagination, got %d", callCount)
	}

	foundFirst := false
	foundSecond := false
	for _, inv := range invoices {
		if inv.InvoiceID == "inv-12345" {
			foundFirst = true
		} else if inv.InvoiceID == "inv-67890" {
			foundSecond = true
		}
	}

	if !foundFirst {
		t.Error("First invoice not found in results")
	}
	if !foundSecond {
		t.Error("Second invoice not found in results")
	}
}

func TestGetFastlyCostsForWindow(t *testing.T) {
	// Arrange
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return createMockResponse(200, `{
                "data": [
                    {
                        "customer_id": "test-customer",
                        "invoice_id": "inv-12345",
                        "billing_start_date": "2024-01-01T00:00:00Z",
                        "billing_end_date": "2024-01-31T00:00:00Z",
                        "transaction_line_items": [
                            {
                                "description": "CDN Bandwidth",
                                "amount": 100.50,
                                "rate": 0.05,
                                "units": 2010,
                                "product_name": "CDN",
                                "product_group": "Full Site Delivery",
                                "region": "Global",
                                "usage_type": "bandwidth"
                            }
                        ]
                    }
                ],
                "meta": {
                    "next_cursor": "",
                    "limit": 200,
                    "total": 1
                }
            }`), nil
		},
	}

	rateLimiter := rate.NewLimiter(rate.Every(time.Second), 10)
	fastlyCostSrc := FastlyCostSource{
		apiKey:       "test-api-key",
		httpClient:   mockClient,
		rateLimiter:  rateLimiter,
		invoiceCache: make(map[string][]fastlyplugin.Invoice),
	}

	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	window := opencost.NewWindow(&startDate, &endDate)

	// Act
	// First get invoices for the period to pass to getFastlyCostsForWindow
	invoices, err := fastlyCostSrc.getInvoicesForPeriod(&startDate, &endDate)
	if err != nil {
		t.Fatalf("Failed to get invoices: %v", err)
	}
	result := fastlyCostSrc.getFastlyCostsForWindow(window, invoices)

	// Assert
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if len(result.Costs) != 1 {
		t.Errorf("Expected 1 cost item, got %d", len(result.Costs))
	}
	if len(result.Errors) > 0 {
		t.Errorf("Unexpected errors in response: %v", result.Errors)
	}

	// Check cost properties
	if len(result.Costs) > 0 {
		cost := result.Costs[0]
		if cost.ResourceType != "Full Site Delivery" {
			t.Errorf("Expected resource type 'Full Site Delivery', got '%s'", cost.ResourceType)
		}
		if cost.UsageUnit != "GB" {
			t.Errorf("Expected usage unit 'GB' for bandwidth, got '%s'", cost.UsageUnit)
		}
	}
}

// Test GetCustomCosts with validation errors
func TestGetCustomCostsWithValidationErrors(t *testing.T) {
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Should not be called due to validation errors
			t.Error("HTTP client should not be called when validation fails")
			return nil, fmt.Errorf("should not be called")
		},
	}

	rateLimiter := rate.NewLimiter(rate.Every(time.Second), 10)
	fastlyCostSrc := FastlyCostSource{
		apiKey:       "test-api-key",
		httpClient:   mockClient,
		rateLimiter:  rateLimiter,
		invoiceCache: make(map[string][]fastlyplugin.Invoice),
	}

	// Create request with multiple validation errors (non-future)
	now := time.Now()
	req := &pb.CustomCostRequest{
		Start:      timestamppb.New(now.AddDate(0, -13, 0)), // Too far in past
		End:        timestamppb.New(now.AddDate(0, -14, 0)), // Before start
		Resolution: durationpb.New(30 * time.Minute),        // Too small
	}

	// Act
	responses := fastlyCostSrc.GetCustomCosts(req)

	// Assert
	if len(responses) != 1 {
		t.Fatalf("Expected 1 response with errors, got %d", len(responses))
	}

	if len(responses[0].Errors) != 3 {
		t.Errorf("Expected 3 validation errors, got %d: %v",
			len(responses[0].Errors), responses[0].Errors)
	}

	// Verify error messages
	expectedErrors := []string{
		"Resolution should be at least one hour",
		"more than 12 months in the past",
		"End date cannot be before start date",
	}

	for _, expected := range expectedErrors {
		found := false
		for _, err := range responses[0].Errors {
			if strings.Contains(err, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected error containing '%s' not found in %v",
				expected, responses[0].Errors)
		}
	}
}

// Test GetCustomCosts with successful response
func TestGetCustomCostsSuccess(t *testing.T) {
	// Create valid request with recent dates (within last 12 months)
	now := time.Now()
	startDate := now.AddDate(0, -1, 0).Truncate(24 * time.Hour) // 1 month ago
	endDate := startDate.Add(24 * time.Hour)                    // 1 day later

	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Use the dynamic start and end dates for the response
			responseBody := fmt.Sprintf(`{
                "data": [
                    {
                        "customer_id": "test-customer",
                        "invoice_id": "inv-12345",
                        "billing_start_date": "%s",
                        "billing_end_date": "%s",
                        "currency_code": "USD",
                        "monthly_transaction_amount": 100.50,
                        "transaction_line_items": [
                            {
                                "description": "CDN Bandwidth",
                                "amount": 100.50,
                                "rate": 0.05,
                                "units": 2010,
                                "product_name": "CDN",
                                "product_group": "Full Site Delivery",
                                "region": "Global",
                                "usage_type": "bandwidth"
                            }
                        ]
                    }
                ],
                "meta": {
                    "next_cursor": "",
                    "limit": 200,
                    "total": 1
                }
            }`, startDate.Format(time.RFC3339), endDate.Format(time.RFC3339))
			return createMockResponse(200, responseBody), nil
		},
	}

	rateLimiter := rate.NewLimiter(rate.Every(time.Second), 10)
	fastlyCostSrc := FastlyCostSource{
		apiKey:       "test-api-key",
		httpClient:   mockClient,
		rateLimiter:  rateLimiter,
		invoiceCache: make(map[string][]fastlyplugin.Invoice),
	}
	req := &pb.CustomCostRequest{
		Start:      timestamppb.New(startDate),
		End:        timestamppb.New(endDate),
		Resolution: durationpb.New(24 * time.Hour),
	}

	// Act
	responses := fastlyCostSrc.GetCustomCosts(req)

	// Assert
	if len(responses) != 1 {
		t.Fatalf("Expected 1 response, got %d", len(responses))
	}

	if len(responses[0].Errors) > 0 {
		t.Errorf("Expected no errors, got: %v", responses[0].Errors)
	}

	if responses[0].Domain != "fastly" {
		t.Errorf("Expected domain 'fastly', got %s", responses[0].Domain)
	}

	if len(responses[0].Costs) != 1 {
		t.Errorf("Expected 1 cost, got %d", len(responses[0].Costs))
	}
}

// Test caching behavior
func TestInvoiceCaching(t *testing.T) {
	callCount := 0
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			callCount++
			return createMockResponse(200, `{
                "data": [
                    {
                        "customer_id": "test-customer",
                        "invoice_id": "inv-cached",
                        "billing_start_date": "2024-01-01T00:00:00Z",
                        "billing_end_date": "2024-01-31T00:00:00Z",
                        "transaction_line_items": []
                    }
                ],
                "meta": {
                    "next_cursor": "",
                    "limit": 200,
                    "total": 1
                }
            }`), nil
		},
	}

	rateLimiter := rate.NewLimiter(rate.Every(time.Second), 10)
	fastlyCostSrc := FastlyCostSource{
		apiKey:       "test-api-key",
		httpClient:   mockClient,
		rateLimiter:  rateLimiter,
		invoiceCache: make(map[string][]fastlyplugin.Invoice),
	}

	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	// First call - should hit the API
	invoices1, err1 := fastlyCostSrc.getInvoicesForPeriod(&startDate, &endDate)
	if err1 != nil {
		t.Fatalf("First call failed: %v", err1)
	}

	// Second call - should use cache
	invoices2, err2 := fastlyCostSrc.getInvoicesForPeriod(&startDate, &endDate)
	if err2 != nil {
		t.Fatalf("Second call failed: %v", err2)
	}

	// Assert
	if callCount != 1 {
		t.Errorf("Expected 1 API call (second should use cache), got %d", callCount)
	}

	if len(invoices1) != len(invoices2) {
		t.Errorf("Cached response differs from original")
	}

	if invoices1[0].InvoiceID != invoices2[0].InvoiceID {
		t.Errorf("Cached invoice ID differs from original")
	}
}

func TestBoilerplateFastlyCustomCost(t *testing.T) {
	// Arrange
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	window := opencost.NewWindow(&startDate, &endDate)

	// Act
	result := boilerplateFastlyCustomCost(window)

	// Assert
	if result.Domain != "fastly" {
		t.Errorf("Expected domain 'fastly', got '%s'", result.Domain)
	}
	if result.CostSource != "billing" {
		t.Errorf("Expected cost source 'billing', got '%s'", result.CostSource)
	}
	if result.Currency != "USD" {
		t.Errorf("Expected currency 'USD', got '%s'", result.Currency)
	}
	if result.Metadata["plugin_version"] != "v1.1.0" {
		t.Errorf("Expected plugin version 'v1.1.0', got '%s'", result.Metadata["plugin_version"])
	}
	if result.Start.AsTime().Format(time.RFC3339) != startDate.Format(time.RFC3339) {
		t.Errorf("Expected start time %s, got %s", startDate.Format(time.RFC3339), result.Start.AsTime().Format(time.RFC3339))
	}
	if result.End.AsTime().Format(time.RFC3339) != endDate.Format(time.RFC3339) {
		t.Errorf("Expected end time %s, got %s", endDate.Format(time.RFC3339), result.End.AsTime().Format(time.RFC3339))
	}
	if len(result.Errors) != 0 {
		t.Errorf("Expected empty errors, got %v", result.Errors)
	}
	if len(result.Costs) != 0 {
		t.Errorf("Expected empty costs, got %d costs", len(result.Costs))
	}
}

// Test GetCustomCosts with future dates returns empty response
func TestGetCustomCostsFutureRequest(t *testing.T) {
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Should not be called for future requests
			t.Error("HTTP client should not be called for future requests")
			return nil, fmt.Errorf("should not be called")
		},
	}

	rateLimiter := rate.NewLimiter(rate.Every(time.Second), 10)
	fastlyCostSrc := FastlyCostSource{
		apiKey:       "test-api-key",
		httpClient:   mockClient,
		rateLimiter:  rateLimiter,
		invoiceCache: make(map[string][]fastlyplugin.Invoice),
	}

	// Create request for future dates
	now := time.Now().UTC().Truncate(time.Hour)
	req := &pb.CustomCostRequest{
		Start:      timestamppb.New(now.Add(time.Hour)),     // 1 hour in the future
		End:        timestamppb.New(now.Add(2 * time.Hour)), // 2 hours in the future
		Resolution: durationpb.New(time.Hour),
	}

	// Act
	responses := fastlyCostSrc.GetCustomCosts(req)

	// Assert
	if len(responses) != 0 {
		t.Fatalf("Expected empty response for future request, got %d responses", len(responses))
	}
}

// Test that each cost item gets a unique UUID
func TestUniqueUUIDGeneration(t *testing.T) {
	// Create valid request
	now := time.Now()
	startDate := now.AddDate(0, -1, 0).Truncate(24 * time.Hour)
	endDate := startDate.Add(24 * time.Hour)

	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Return mock response with multiple line items from the same invoice
			responseBody := fmt.Sprintf(`{
                "data": [
                    {
                        "customer_id": "test-customer",
                        "invoice_id": "inv-12345",
                        "billing_start_date": "%s",
                        "billing_end_date": "%s",
                        "currency_code": "USD",
                        "transaction_line_items": [`, startDate.Format(time.RFC3339), endDate.Format(time.RFC3339))
			responseBody += `
                            {
                                "description": "CDN Bandwidth",
                                "amount": 100.50,
                                "rate": 0.05,
                                "units": 2010,
                                "product_name": "CDN",
                                "product_group": "Full Site Delivery",
                                "usage_type": "bandwidth"
                            },
                            {
                                "description": "Compute Requests",
                                "amount": 25.75,
                                "rate": 0.001,
                                "units": 25750,
                                "product_name": "Compute",
                                "product_group": "Edge Computing",
                                "usage_type": "requests"
                            },
                            {
                                "description": "WAF Requests",
                                "amount": 15.25,
                                "rate": 0.0005,
                                "units": 30500,
                                "product_name": "WAF",
                                "product_group": "Security",
                                "usage_type": "requests"
                            }
                        ]
                    }
                ],
                "meta": {
                    "next_cursor": "",
                    "limit": 200,
                    "total": 1
                }
            }`
			return createMockResponse(200, responseBody), nil
		},
	}

	rateLimiter := rate.NewLimiter(rate.Every(time.Second), 10)
	fastlyCostSrc := FastlyCostSource{
		apiKey:       "test-api-key",
		httpClient:   mockClient,
		rateLimiter:  rateLimiter,
		invoiceCache: make(map[string][]fastlyplugin.Invoice),
	}

	req := &pb.CustomCostRequest{
		Start:      timestamppb.New(startDate),
		End:        timestamppb.New(endDate),
		Resolution: durationpb.New(24 * time.Hour),
	}

	// Act
	responses := fastlyCostSrc.GetCustomCosts(req)

	// Assert
	if len(responses) != 1 {
		t.Fatalf("Expected 1 response, got %d", len(responses))
	}

	if len(responses[0].Costs) != 3 {
		t.Fatalf("Expected 3 costs (3 line items), got %d", len(responses[0].Costs))
	}

	// Check that all IDs are unique and are valid UUIDs
	seenIDs := make(map[string]bool)
	for i, cost := range responses[0].Costs {
		// Check ID is not empty
		if cost.Id == "" {
			t.Errorf("Cost %d has empty ID", i)
		}

		// Check ID is unique
		if seenIDs[cost.Id] {
			t.Errorf("Duplicate ID found: %s", cost.Id)
		}
		seenIDs[cost.Id] = true

		// Check ID looks like a UUID (contains hyphens and is 36 characters)
		if len(cost.Id) != 36 {
			t.Errorf("Cost %d ID '%s' is not 36 characters (expected UUID format)", i, cost.Id)
		}
		if !strings.Contains(cost.Id, "-") {
			t.Errorf("Cost %d ID '%s' doesn't contain hyphens (expected UUID format)", i, cost.Id)
		}

		// Verify ProviderId is constructed with CustomerID (not InvoiceID) for stability
		expectedProviderID := fmt.Sprintf("test-customer/%s/%s", cost.Labels["product_name"], cost.ResourceName)
		if cost.ProviderId != expectedProviderID {
			t.Errorf("Cost %d has unexpected ProviderId. Expected %s, got %s", i, expectedProviderID, cost.ProviderId)
		}

		// Verify ID is different from ProviderId
		if cost.Id == cost.ProviderId {
			t.Errorf("Cost %d ID should be different from ProviderId, both are: %s", i, cost.Id)
		}
	}

	t.Logf("Successfully generated %d unique UUIDs for cost items", len(responses[0].Costs))
}

func TestGetInvoiceByID(t *testing.T) {
	// Arrange
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Verify the request
			if req.Method != "GET" {
				t.Errorf("Expected GET request, got %s", req.Method)
			}
			if !strings.Contains(req.URL.Path, "/billing/v3/invoices/inv-12345") {
				t.Errorf("Expected invoice by ID endpoint, got %s", req.URL.Path)
			}
			if req.Header.Get("Fastly-Key") != "test-api-key" {
				t.Errorf("Expected API key in header")
			}

			return createMockResponse(200, `{
                "customer_id": "test-customer",
                "invoice_id": "inv-12345",
                "billing_start_date": "2024-01-01T00:00:00Z",
                "billing_end_date": "2024-01-31T00:00:00Z",
                "currency_code": "USD",
                "transaction_line_items": [
                    {
                        "description": "CDN Bandwidth",
                        "amount": 100.50,
                        "rate": 0.05,
                        "units": 2010,
                        "product_name": "CDN",
                        "product_group": "Full Site Delivery",
                        "product_line": "Delivery",
                        "usage_type": "bandwidth",
                        "region": "North America"
                    }
                ]
            }`), nil
		},
	}

	rateLimiter := rate.NewLimiter(rate.Every(time.Second), 10)
	fastlyCostSrc := FastlyCostSource{
		apiKey:       "test-api-key",
		httpClient:   mockClient,
		rateLimiter:  rateLimiter,
		invoiceCache: make(map[string][]fastlyplugin.Invoice),
	}

	// Act
	invoice, err := fastlyCostSrc.getInvoiceByID("inv-12345")

	// Assert
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if invoice == nil {
		t.Fatal("Expected invoice, got nil")
	}

	if invoice.InvoiceID != "inv-12345" {
		t.Errorf("Expected invoice ID 'inv-12345', got '%s'", invoice.InvoiceID)
	}

	if invoice.CustomerID != "test-customer" {
		t.Errorf("Expected customer ID 'test-customer', got '%s'", invoice.CustomerID)
	}

	if len(invoice.TransactionLineItems) != 1 {
		t.Errorf("Expected 1 line item, got %d", len(invoice.TransactionLineItems))
	}

	lineItem := invoice.TransactionLineItems[0]
	if lineItem.Description != "CDN Bandwidth" {
		t.Errorf("Expected description 'CDN Bandwidth', got '%s'", lineItem.Description)
	}

	if lineItem.Amount != 100.50 {
		t.Errorf("Expected amount 100.50, got %f", lineItem.Amount)
	}
}

func TestGetInvoiceByIDError(t *testing.T) {
	// Arrange
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Verify the request URL contains the invoice ID
			if !strings.Contains(req.URL.Path, "/billing/v3/invoices/invalid-invoice") {
				t.Errorf("Expected invoice by ID endpoint with invalid-invoice, got %s", req.URL.Path)
			}

			return createMockResponse(404, `{
                "msg": "Not Found",
                "detail": "Invoice not found"
            }`), nil
		},
	}

	rateLimiter := rate.NewLimiter(rate.Every(time.Second), 10)
	fastlyCostSrc := FastlyCostSource{
		apiKey:       "test-api-key",
		httpClient:   mockClient,
		rateLimiter:  rateLimiter,
		invoiceCache: make(map[string][]fastlyplugin.Invoice),
	}

	// Act
	invoice, err := fastlyCostSrc.getInvoiceByID("invalid-invoice")

	// Assert
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if invoice != nil {
		t.Error("Expected nil invoice on error")
	}

	if !strings.Contains(err.Error(), "404") {
		t.Errorf("Expected error message to contain '404', got: %v", err)
	}
}

func TestGetInvoiceByIDNetworkError(t *testing.T) {
	// Arrange
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("network connection failed")
		},
	}

	rateLimiter := rate.NewLimiter(rate.Every(time.Second), 10)
	fastlyCostSrc := FastlyCostSource{
		apiKey:       "test-api-key",
		httpClient:   mockClient,
		rateLimiter:  rateLimiter,
		invoiceCache: make(map[string][]fastlyplugin.Invoice),
	}

	// Act
	invoice, err := fastlyCostSrc.getInvoiceByID("inv-12345")

	// Assert
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if invoice != nil {
		t.Error("Expected nil invoice on network error")
	}

	if !strings.Contains(err.Error(), "network connection failed") {
		t.Errorf("Expected error message to contain 'network connection failed', got: %v", err)
	}
}

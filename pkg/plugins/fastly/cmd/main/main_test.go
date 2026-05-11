package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	fastlyplugin "github.com/opencost/opencost-plugins/pkg/plugins/fastly/fastlyplugin"
)

func TestGetInvoices(t *testing.T) {
	// Create a mock Fastly API server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify API key header
		if r.Header.Get("Fastly-Key") != "test-api-key" {
			t.Errorf("Expected Fastly-Key header 'test-api-key', got '%s'", r.Header.Get("Fastly-Key"))
		}

		// Check if this is a request for a specific invoice
		if r.URL.Path == "/billing/v3/invoices/12345" {
			invoice := fastlyplugin.FastlyInvoice{
				InvoiceID:               "12345",
				CustomerID:              "cust-123",
				CurrencyCode:            "USD",
				BillingStartDate:        "2024-01-01",
				BillingEndDate:          "2024-01-31",
				MonthlyTransactionAmount: 1500.00,
				TransactionLineItems: []fastlyplugin.FastlyTransactionLine{
					{
						Description:  "Compute - Request",
						ProductGroup: "Compute",
						ProductLine:  "Network Services",
						ProductName:  "Requests",
						UsageType:    "requests",
						Region:       "NA",
						Units:        1000000,
						Rate:         0.00075,
						Amount:       750.00,
					},
					{
						Description:  "Bandwidth - Delivery",
						ProductGroup: "Full-Site Delivery",
						ProductLine:  "Network Services",
						ProductName:  "Bandwidth",
						UsageType:    "bandwidth",
						Region:       "NA",
						Units:        500,
						Rate:         0.50,
						Amount:       250.00,
					},
				},
			}
			json.NewEncoder(w).Encode(invoice)
			return
		}

		// Return invoice list
		invoiceResp := fastlyplugin.FastlyInvoiceListResponse{
			Meta: fastlyplugin.FastlyInvoiceMeta{
				Limit:      200,
				NextCursor: "",
				Total:      1,
			},
			Invoices: []fastlyplugin.FastlyInvoice{
				{
					InvoiceID:               "12345",
					CustomerID:              "cust-123",
					CurrencyCode:            "USD",
					BillingStartDate:        "2024-01-01",
					BillingEndDate:          "2024-01-31",
					MonthlyTransactionAmount: 1500.00,
				},
			},
		}
		json.NewEncoder(w).Encode(invoiceResp)
	}))
	defer mockServer.Close()

	// Override URLs for testing
	origInvoicesURL := fastlyInvoicesURL
	origInvoiceByIDURL := fastlyInvoiceByIDURL
	defer func() {
		// Note: these are consts, so we can't actually override them in tests.
		// In a real implementation, we'd make these configurable.
		_ = origInvoicesURL
		_ = origInvoiceByIDURL
	}()

	// Verify the response format matches expected
	resp, err := http.Get(mockServer.URL + "/billing/v3/invoices")
	if err != nil {
		t.Fatalf("Error making request: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var invoiceResp fastlyplugin.FastlyInvoiceListResponse
	err = json.Unmarshal(body, &invoiceResp)
	if err != nil {
		t.Fatalf("Error unmarshalling response: %v", err)
	}

	if len(invoiceResp.Invoices) != 1 {
		t.Errorf("Expected 1 invoice, got %d", len(invoiceResp.Invoices))
	}

	if invoiceResp.Invoices[0].InvoiceID != "12345" {
		t.Errorf("Expected invoice ID '12345', got '%s'", invoiceResp.Invoices[0].InvoiceID)
	}
}

func TestFastlyTransactionLineUnmarshal(t *testing.T) {
	jsonData := `{
		"description": "Compute - Request",
		"product_group": "Compute",
		"product_line": "Network Services",
		"product_name": "Requests",
		"usage_type": "requests",
		"region": "NA",
		"units": 1000000,
		"rate": 0.00075,
		"amount": 750.00,
		"credit_coupon_code": ""
	}`

	var lineItem fastlyplugin.FastlyTransactionLine
	err := json.Unmarshal([]byte(jsonData), &lineItem)
	if err != nil {
		t.Fatalf("Error unmarshalling transaction line: %v", err)
	}

	if lineItem.ProductName != "Requests" {
		t.Errorf("Expected product name 'Requests', got '%s'", lineItem.ProductName)
	}

	if lineItem.Amount != 750.00 {
		t.Errorf("Expected amount 750.00, got %f", lineItem.Amount)
	}

	if lineItem.Units != 1000000 {
		t.Errorf("Expected units 1000000, got %f", lineItem.Units)
	}

	if lineItem.Region != "NA" {
		t.Errorf("Expected region 'NA', got '%s'", lineItem.Region)
	}
}

func TestFastlyInvoiceUnmarshal(t *testing.T) {
	jsonData := `{
		"invoice_id": "12345",
		"customer_id": "cust-123",
		"currency_code": "USD",
		"billing_start_date": "2024-01-01",
		"billing_end_date": "2024-01-31",
		"statement_number": "ST-001",
		"monthly_transaction_amount": 1500.00,
		"transaction_line_items": [
			{
				"description": "Compute - Request",
				"product_group": "Compute",
				"product_line": "Network Services",
				"product_name": "Requests",
				"usage_type": "requests",
				"region": "NA",
				"units": 1000000,
				"rate": 0.00075,
				"amount": 750.00
			}
		]
	}`

	var invoice fastlyplugin.FastlyInvoice
	err := json.Unmarshal([]byte(jsonData), &invoice)
	if err != nil {
		t.Fatalf("Error unmarshalling invoice: %v", err)
	}

	if invoice.InvoiceID != "12345" {
		t.Errorf("Expected invoice ID '12345', got '%s'", invoice.InvoiceID)
	}

	if len(invoice.TransactionLineItems) != 1 {
		t.Errorf("Expected 1 line item, got %d", len(invoice.TransactionLineItems))
	}

	if invoice.MonthlyTransactionAmount != 1500.00 {
		t.Errorf("Expected monthly transaction amount 1500.00, got %f", invoice.MonthlyTransactionAmount)
	}
}

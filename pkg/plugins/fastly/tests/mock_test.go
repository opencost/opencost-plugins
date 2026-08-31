package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/opencost/opencost-plugins/pkg/plugins/fastly/fastlyplugin"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Mock Fastly API server for testing
func createMockFastlyServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/billing/v3/invoices":
			// Mock invoice list response
			response := fastlyplugin.InvoiceListResponse{
				Data: []fastlyplugin.Invoice{
					{
						CustomerID:               "mock-customer-123",
						InvoiceID:                "inv-mock-001",
						InvoicePostedOn:          "2024-01-01T00:00:00Z",
						BillingStartDate:         "2024-01-01T00:00:00Z",
						BillingEndDate:           "2024-01-31T23:59:59Z",
						StatementNumber:          "STMT-001",
						CurrencyCode:             "USD",
						MonthlyTransactionAmount: "150.75",
						PaymentStatus:            "paid",
						TransactionLineItems: []fastlyplugin.TransactionLineItem{
							{
								Description:      "CDN Bandwidth - North America",
								Amount:           75.50,
								CreditCouponCode: "",
								Rate:             0.12,
								Units:            629.17,
								ProductName:      "Full Site Delivery",
								ProductGroup:     "Full Site Delivery",
								ProductLine:      "CDN",
								Region:           "North America",
								UsageType:        "bandwidth",
							},
							{
								Description:      "Image Optimization",
								Amount:           25.25,
								CreditCouponCode: "",
								Rate:             0.05,
								Units:            505.0,
								ProductName:      "Image Optimization",
								ProductGroup:     "Full Site Delivery",
								ProductLine:      "CDN",
								Region:           "Global",
								UsageType:        "image_processing",
							},
							{
								Description:      "Compute@Edge",
								Amount:           50.00,
								CreditCouponCode: "",
								Rate:             1.00,
								Units:            50.0,
								ProductName:      "Compute@Edge",
								ProductGroup:     "Compute",
								ProductLine:      "Edge Compute",
								Region:           "Global",
								UsageType:        "compute_time",
							},
						},
					},
				},
				Meta: fastlyplugin.PaginationMetadata{
					NextCursor: "",
					Limit:      200,
					Total:      1,
					Sort:       "billing_start_date",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		case "/billing/v3/invoices/month-to-date":
			// Mock month-to-date response
			invoice := fastlyplugin.Invoice{
				CustomerID:               "mock-customer-123",
				InvoiceID:                "mtd-mock-001",
				InvoicePostedOn:          time.Now().Format(time.RFC3339),
				BillingStartDate:         time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
				BillingEndDate:           time.Now().Format(time.RFC3339),
				StatementNumber:          "MTD-001",
				CurrencyCode:             "USD",
				MonthlyTransactionAmount: "45.25",
				PaymentStatus:            "pending",
				TransactionLineItems: []fastlyplugin.TransactionLineItem{
					{
						Description:      "CDN Bandwidth - Current Month",
						Amount:           45.25,
						CreditCouponCode: "",
						Rate:             0.12,
						Units:            377.08,
						ProductName:      "Full Site Delivery",
						ProductGroup:     "Full Site Delivery",
						ProductLine:      "CDN",
						Region:           "North America",
						UsageType:        "bandwidth",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(invoice)

		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"msg": "Not Found"}`))
		}
	}))
}

// Test with mock data instead of real API
func TestFastlyMockCostRetrieval(t *testing.T) {
	// Create mock server
	mockServer := createMockFastlyServer()
	defer mockServer.Close()

	// Create temp config file
	config := fastlyplugin.FastlyConfig{
		FastlyAPIKey: "mock-api-key",
		LogLevel:     "debug",
	}

	configFile, err := os.CreateTemp("", "fastly_mock_config.json")
	if err != nil {
		t.Fatalf("could not create temp config file: %v", err)
	}
	defer os.Remove(configFile.Name())

	configData, err := json.MarshalIndent(config, "", " ")
	if err != nil {
		t.Fatalf("could not marshal config: %v", err)
	}

	if _, err := configFile.Write(configData); err != nil {
		t.Fatalf("could not write config file: %v", err)
	}
	configFile.Close()

	// Set up test parameters
	now := time.Now()
	windowStart := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	// Note: You would need to modify the main plugin code to accept a custom HTTP client
	// or base URL for testing. This is a limitation of the current implementation.
	// For now, we can test the data structures and basic functionality without the full request.
	
	t.Log("Mock server created successfully. To fully test, you would need to:")
	t.Log("1. Modify the FastlyCostSource to accept a custom base URL")
	t.Log("2. Or inject a custom HTTP client")
	t.Log("3. Then point it to the mock server URL:", mockServer.URL)
	
	// For now, we can test the data structures and basic functionality
	testInvoice := fastlyplugin.Invoice{
		CustomerID:               "test-customer",
		InvoiceID:                "test-invoice-001", 
		MonthlyTransactionAmount: "100.50",
		BillingStartDate:         windowStart.Format(time.RFC3339),
		BillingEndDate:           windowEnd.Format(time.RFC3339),
		CurrencyCode:             "USD",
		TransactionLineItems: []fastlyplugin.TransactionLineItem{
			{
				Description:  "Test CDN Usage",
				Amount:       100.50,
				Rate:         0.12,
				Units:        837.5,
				ProductName:  "Full Site Delivery",
				ProductGroup: "Full Site Delivery",
				ProductLine:  "CDN",
				Region:       "North America",
				UsageType:    "bandwidth",
			},
		},
	}

	// Test JSON marshaling/unmarshaling
	data, err := json.Marshal(testInvoice)
	if err != nil {
		t.Fatalf("failed to marshal test invoice: %v", err)
	}

	var unmarshaled fastlyplugin.Invoice
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal test invoice: %v", err)
	}

	// Verify the monthly transaction amount is preserved as string
	if unmarshaled.MonthlyTransactionAmount != "100.50" {
		t.Errorf("expected MonthlyTransactionAmount to be '100.50', got %s", unmarshaled.MonthlyTransactionAmount)
	}

	// Test with numeric input (simulating API response)
	jsonWithNumber := `{
		"customer_id": "test-customer",
		"invoice_id": "test-invoice-001",
		"monthly_transaction_amount": 100.50,
		"billing_start_date": "2024-01-01T00:00:00Z",
		"billing_end_date": "2024-01-31T23:59:59Z",
		"currency_code": "USD",
		"transaction_line_items": []
	}`

	var invoiceFromNumber fastlyplugin.Invoice
	if err := json.Unmarshal([]byte(jsonWithNumber), &invoiceFromNumber); err != nil {
		t.Fatalf("failed to unmarshal invoice with numeric monthly_transaction_amount: %v", err)
	}

	// Verify numeric input is converted to string
	if invoiceFromNumber.MonthlyTransactionAmount != "100.5" {
		t.Errorf("expected MonthlyTransactionAmount to be '100.5', got %s", invoiceFromNumber.MonthlyTransactionAmount)
	}

	t.Log("Mock data tests passed successfully!")
}

// Generate test protobuf files for validator testing
func TestGenerateValidatorTestData(t *testing.T) {
	// Create mock daily response
	now := time.Now()
	dailyStart := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, time.UTC)
	dailyEnd := dailyStart.AddDate(0, 0, 1)

	dailyResponse := &pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v3"},
		CostSource: "billing",
		Domain:     "fastly",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(dailyStart),
		End:        timestamppb.New(dailyEnd),
		Errors:     []string{},
		Costs: []*pb.CustomCost{
			{
				Zone:           "North America",
				AccountName:    "test-customer-123",
				ChargeCategory: "usage",
				Description:    "CDN Bandwidth Usage",
				ResourceName:   "bandwidth",
				ResourceType:   "Full Site Delivery",
				Id:             "inv-test-001",
				ProviderId:     "inv-test-001/Full Site Delivery/bandwidth",
				Labels: map[string]string{
					"product_line": "CDN",
					"product_name": "Full Site Delivery",
				},
				ListCost:      75.50,
				ListUnitPrice: 0.12,
				BilledCost:    75.50,
				UsageQuantity: 629.17,
				UsageUnit:     "GB",
			},
			{
				Zone:           "Global",
				AccountName:    "test-customer-123",
				ChargeCategory: "usage",
				Description:    "Edge Compute Usage",
				ResourceName:   "compute_time",
				ResourceType:   "Compute",
				Id:             "inv-test-001",
				ProviderId:     "inv-test-001/Compute@Edge/compute_time",
				Labels: map[string]string{
					"product_line": "Edge Compute",
					"product_name": "Compute@Edge",
				},
				ListCost:      50.00,
				ListUnitPrice: 1.00,
				BilledCost:    50.00,
				UsageQuantity: 50.0,
				UsageUnit:     "hours",
			},
		},
	}

	// Create mock hourly response (smaller amounts)
	hourlyStart := dailyStart
	hourlyEnd := hourlyStart.Add(time.Hour)

	hourlyResponse := &pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v3"},
		CostSource: "billing",
		Domain:     "fastly",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(hourlyStart),
		End:        timestamppb.New(hourlyEnd),
		Errors:     []string{},
		Costs: []*pb.CustomCost{
			{
				Zone:           "North America",
				AccountName:    "test-customer-123", 
				ChargeCategory: "usage",
				Description:    "CDN Bandwidth Usage - Hourly",
				ResourceName:   "bandwidth",
				ResourceType:   "Full Site Delivery",
				Id:             "inv-test-hourly-001",
				ProviderId:     "inv-test-hourly-001/Full Site Delivery/bandwidth",
				Labels: map[string]string{
					"product_line": "CDN",
					"product_name": "Full Site Delivery",
				},
				ListCost:      3.15,
				ListUnitPrice: 0.12,
				BilledCost:    3.15,
				UsageQuantity: 26.25,
				UsageUnit:     "GB",
			},
		},
	}

	// Create temporary files for testing
	dailyFile, err := os.CreateTemp("", "fastly_daily_test.json")
	if err != nil {
		t.Fatalf("could not create daily test file: %v", err)
	}
	defer os.Remove(dailyFile.Name())

	hourlyFile, err := os.CreateTemp("", "fastly_hourly_test.json")
	if err != nil {
		t.Fatalf("could not create hourly test file: %v", err)
	}
	defer os.Remove(hourlyFile.Name())

	// Write responses as JSON arrays (as expected by validator)
	dailyData, _ := json.MarshalIndent([]*pb.CustomCostResponse{dailyResponse}, "", "  ")
	hourlyData, _ := json.MarshalIndent([]*pb.CustomCostResponse{hourlyResponse}, "", "  ")

	dailyFile.Write(dailyData)
	hourlyFile.Write(hourlyData)
	dailyFile.Close()
	hourlyFile.Close()

	t.Logf("Test data files created:")
	t.Logf("Daily: %s", dailyFile.Name())
	t.Logf("Hourly: %s", hourlyFile.Name())
	t.Logf("\nTo test the validator, run:")
	t.Logf("go run ./cmd/validator/main/main.go %s %s", dailyFile.Name(), hourlyFile.Name())
}
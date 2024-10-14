package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	atlasplugin "github.com/opencost/opencost-plugins/pkg/plugins/mongodb-atlas/plugin"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/stretchr/testify/assert"
)

// Mock HTTPClient implementation
type MockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

// The MockHTTPClient's Do method uses a function defined at runtime to mock various responses
func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.DoFunc(req)
}

// FOR INTEGRATION TESTING PURPOSES ONLY
// expects 3 env variables to be set to work
// mapuk = public key for mongodb atlas
// maprk  = private key for mongodb atlas
// maOrgId = orgId to be testsed
// func TestMain(t *testing.T) {

// 	publicKey := os.Getenv("mapuk")
// 	privateKey := os.Getenv("maprk")
// 	orgId := os.Getenv("maorgid")
// 	if publicKey == "" || privateKey == "" || orgId == "" {
// 		t.Skip("Skipping integration test.")
// 	}

// 	assert.NotNil(t, publicKey)
// 	assert.NotNil(t, privateKey)
// 	assert.NotNil(t, orgId)

// 	client := &http.Client{
// 		Transport: &digest.Transport{
// 			Username: publicKey,
// 			Password: privateKey,
// 		},
// 	}

// 	// Define the layout that matches the format of the date string
// 	layout := "2006-01-02"
// 	endTime, _ := time.Parse(layout, "2024-07-01")
// 	startTime, _ := time.Parse(layout, "2023-12-01")
// 	url := "https://cloud.mongodb.com/api/atlas/v2/orgs/" + orgId + "/billing/costExplorer/usage"
// 	resp, err := createCostExplorerQueryToken(orgId, startTime, endTime, client)

// 	assert.NotEmpty(t, resp)
// 	assert.Nil(t, err)

// }

// tests for getCosts
func TestGetCostsPendingInvoices(t *testing.T) {
	pendingInvoiceResponse := atlasplugin.PendingInvoice{
		AmountBilledCents: 0,
		AmountPaidCents:   0,
		Created:           "2024-10-01T02:00:26Z",
		CreditsCents:      0,
		EndDate:           "2024-11-01T00:00:00Z",
		Id:                "66fb726b79b56205f9376437",
		LineItems: []atlasplugin.LineItem{
			{
				ClusterName:      "kubecost-mongo-dev-1",
				Created:          "2024-10-11T02:57:56Z",
				EndDate:          "2024-10-11T00:00:00Z",
				GroupId:          "66d7254246a21a41036ff33e",
				GroupName:        "Project 0",
				Quantity:         6.035e-07,
				SKU:              "ATLAS_AWS_DATA_TRANSFER_DIFFERENT_REGION",
				StartDate:        "2024-10-10T00:00:00Z",
				TotalPriceCents:  0,
				Unit:             "GB",
				UnitPriceDollars: 0.02,
			},
		},
		Links: []atlasplugin.Link{
			{
				Href: "https://cloud.mongodb.com/api/atlas/v2/orgs/66d7254246a21a41036ff2e9",
				Rel:  "self",
			},
		},
		OrgId:                "66d7254246a21a41036ff2e9",
		SalesTaxCents:        0,
		StartDate:            "2024-10-01T00:00:00Z",
		StartingBalanceCents: 0,
		StatusName:           "PENDING",
		SubTotalCents:        0,
		Updated:              "2024-10-01T02:00:26Z",
	}

	mockResponseJson, _ := json.Marshal(pendingInvoiceResponse)

	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Verify that the request method and URL are correct
			if req.Method != http.MethodGet {
				t.Errorf("expected GET request, got %s", req.Method)
			}
			expectedURL := fmt.Sprintf(costExplorerPendingInvoices, "myOrg")
			if req.URL.String() != expectedURL {
				t.Errorf("expected URL %s, got %s", expectedURL, req.URL.String())
			}

			// Return a mock response with status 200 and mock JSON body
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBuffer(mockResponseJson)),
			}, nil
		},
	}
	lineItems, err := GetPendingInvoices("myOrg", mockClient)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(lineItems))

	for _, invoice := range pendingInvoiceResponse.LineItems {
		assert.Equal(t, "kubecost-mongo-dev-1", invoice.ClusterName)
		assert.Equal(t, "66d7254246a21a41036ff33e", invoice.GroupId)
		assert.Equal(t, "Project 0", invoice.GroupName)
		//TODO add more asserts on the fields
	}
}

func TestGetCostErrorFromServer(t *testing.T) {

	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {

			// Return a mock response with status 200 and mock JSON body
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewBufferString("")),
			}, nil
		},
	}
	costs, err := GetPendingInvoices("myOrg", mockClient)

	assert.NotEmpty(t, err)
	assert.Nil(t, costs)

}

func TestGetCostsBadMessage(t *testing.T) {

	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {

			// Return a mock response with status 200 and mock JSON body
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewBufferString("No Jason No")),
			}, nil
		},
	}

	_, error := GetPendingInvoices("myOrd", mockClient)
	assert.NotEmpty(t, error)

}

func TestGetAtlasCostsForWindow(t *testing.T) {

	atlasCostSource := AtlasCostSource{
		orgID: "myOrg",
	}
	// Define the start and end time for the window
	day1 := time.Date(2024, time.October, 12, 0, 0, 0, 0, time.UTC) // Now

	day2 := time.Date(2024, time.October, 13, 0, 0, 0, 0, time.UTC)
	day3 := time.Date(2024, time.October, 14, 0, 0, 0, 0, time.UTC) // Now
	lineItems := []atlasplugin.LineItem{
		{
			ClusterName:      "kubecost-mongo-dev-1",
			Created:          "2024-10-11T02:57:56Z",
			EndDate:          day3.Format("2006-01-02T15:04:05Z07:00"),
			GroupId:          "66d7254246a21a41036ff33e",
			GroupName:        "Project 0",
			Quantity:         6.035e-07,
			SKU:              "ATLAS_AWS_DATA_TRANSFER_DIFFERENT_REGION",
			StartDate:        day2.Format("2006-01-02T15:04:05Z07:00"),
			TotalPriceCents:  0,
			Unit:             "GB",
			UnitPriceDollars: 0.02,
		},
		{
			ClusterName:      "kubecost-mongo-dev-1",
			Created:          "2024-10-11T02:57:56Z",
			EndDate:          day2.Format("2006-01-02T15:04:05Z07:00"),
			GroupId:          "66d7254246a21a41036ff33e",
			GroupName:        "Project 0",
			Quantity:         0.0555,
			SKU:              "ATLAS_AWS_DATA_TRANSFER_DIFFERENT_REGION",
			StartDate:        day1.Add(-24 * time.Hour).Format("2006-01-02T15:04:05Z07:00"),
			TotalPriceCents:  0,
			Unit:             "GB",
			UnitPriceDollars: 0.03,
		},
	}

	// Create a new Window instance
	window := opencost.NewWindow(&day2, &day3)
	resp, error := atlasCostSource.getAtlasCostsForWindow(&window, lineItems)
	assert.Nil(t, error)
	assert.True(t, resp != nil)
	assert.Equal(t, "data_storage", resp.CostSource)
	assert.Equal(t, "mongodb-atlas", resp.Domain)
	assert.Equal(t, "v1", resp.Version)
	assert.Equal(t, "USD", resp.Currency)
	assert.Equal(t, 1, len(resp.Costs))
}

func TestGetCosts(t *testing.T) {
	pendingInvoiceResponse := atlasplugin.PendingInvoice{
		AmountBilledCents: 0,
		AmountPaidCents:   0,
		Created:           "2024-10-01T02:00:26Z",
		CreditsCents:      0,
		EndDate:           "2024-11-01T00:00:00Z",
		Id:                "66fb726b79b56205f9376437",
		LineItems:         []atlasplugin.LineItem{},
		Links: []atlasplugin.Link{
			{
				Href: "https://cloud.mongodb.com/api/atlas/v2/orgs/66d7254246a21a41036ff2e9",
				Rel:  "self",
			},
		},
		OrgId:                "66d7254246a21a41036ff2e9",
		SalesTaxCents:        0,
		StartDate:            "2024-10-01T00:00:00Z",
		StartingBalanceCents: 0,
		StatusName:           "PENDING",
		SubTotalCents:        0,
		Updated:              "2024-10-01T02:00:26Z",
	}

	mockResponseJson, _ := json.Marshal(pendingInvoiceResponse)
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {

			//return costs
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBuffer(mockResponseJson)),
			}, nil

		},
	}
	atlasCostSource := AtlasCostSource{
		orgID:       "myOrg",
		atlasClient: mockClient,
	}
	// Define the start and end time for the window
	now := time.Now()
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	customCostRequest := pb.CustomCostRequest{
		Start:      timestamppb.New(currentMonthStart),                     // Start in current month
		End:        timestamppb.New(currentMonthStart.Add(48 * time.Hour)), // End in current month
		Resolution: durationpb.New(24 * time.Hour),                         // 1 day resolution

	}

	resp := atlasCostSource.GetCustomCosts(&customCostRequest)

	assert.Equal(t, 2, len(resp))
	assert.True(t, len(resp[0].Costs) == 0)
	assert.True(t, len(resp[1].Costs) == 0)
}

func TestValidateRequest(t *testing.T) {
	// Get current time and first day of the current month
	now := time.Now()
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	tests := []struct {
		name           string
		req            *pb.CustomCostRequest
		expectedErrors []string
	}{
		{
			name: "Valid request",
			req: &pb.CustomCostRequest{
				Start:      timestamppb.New(currentMonthStart.Add(5 * time.Hour)),  // Start in current month
				End:        timestamppb.New(currentMonthStart.Add(48 * time.Hour)), // End in current month
				Resolution: durationpb.New(24 * time.Hour),                         // 1 day resolution
			},
			expectedErrors: []string{},
		},
		{
			name: "Resolution less than a day",
			req: &pb.CustomCostRequest{
				Start:      timestamppb.New(currentMonthStart.Add(5 * time.Hour)),  // Start in current month
				End:        timestamppb.New(currentMonthStart.Add(48 * time.Hour)), // End in current month
				Resolution: durationpb.New(12 * time.Hour),                         // 12 hours resolution (error)
			},
			expectedErrors: []string{"Resolution should be at least one day."},
		},
		{
			name: "Start date before current month",
			req: &pb.CustomCostRequest{
				Start:      timestamppb.New(currentMonthStart.Add(-48 * time.Hour)), // Start before current month (error)
				End:        timestamppb.New(currentMonthStart.Add(48 * time.Hour)),  // End in current month
				Resolution: durationpb.New(24 * time.Hour),                          // 1 day resolution
			},
			expectedErrors: []string{"Start date cannot be before the current month. Historical costs not currently supported"},
		},
		{
			name: "End date before current month",
			req: &pb.CustomCostRequest{
				Start:      timestamppb.New(currentMonthStart.Add(5 * time.Hour)),   // Start in current month
				End:        timestamppb.New(currentMonthStart.Add(-48 * time.Hour)), // End before current month (error)
				Resolution: durationpb.New(24 * time.Hour),                          // 1 day resolution
			},
			expectedErrors: []string{"End date cannot be before the current month. Historical costs not currently supported"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := validateRequest(tt.req)

			if len(errors) != len(tt.expectedErrors) {
				t.Errorf("Expected %d errors, got %d", len(tt.expectedErrors), len(errors))
			}

			for i, err := range tt.expectedErrors {
				if errors[i] != err {
					t.Errorf("Expected error %q, got %q", err, errors[i])
				}
			}
		})
	}
}

func TestFilterInvoicesOnWindow(t *testing.T) {
	// Setup test data
	//day3.Format("2006-01-02T15:04:05Z07:00")
	windowStart := time.Date(2024, time.October, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2024, time.October, 31, 0, 0, 0, 0, time.UTC)
	window := opencost.NewWindow(&windowStart, &windowEnd)

	lineItems := []atlasplugin.LineItem{
		{StartDate: "2024-10-05T00:00:00Z", EndDate: "2024-10-10T00:00:00Z", UnitPriceDollars: 1.0, GroupName: "kubecost0",
			SKU: "0", ClusterName: "cluster-0", GroupId: "A", TotalPriceCents: 45, Quantity: 2, Unit: "GB"}, // Within window
		{StartDate: "2024-09-01T00:00:00Z", EndDate: "2024-09-30T00:00:00Z"},                         // Before window
		{StartDate: "2024-11-01T00:00:00Z", EndDate: "2024-11-10T00:00:00Z"},                         // After window
		{StartDate: "2024-10-01T00:00:00Z", EndDate: "2024-10-31T00:00:00Z", UnitPriceDollars: 5},    // Exactly matching the window
		{StartDate: "2024-10-15T00:00:00Z", EndDate: "2024-10-20T00:00:00Z", UnitPriceDollars: 2.45}, // Fully within window
		{StartDate: "2024-09-25T00:00:00Z", EndDate: "2024-10-13T00:00:00Z"},                         // Partially in window
		{StartDate: "2024-10-12T00:00:00Z", EndDate: "2024-11-01T00:00:00Z"},                         // Partially in window
	}

	filteredItems := filterLineItemsByWindow(&window, lineItems)

	// Verify results
	assert.Equal(t, 3, len(filteredItems), "Expected 3 line items to be filtered")

	//Check if the filtered items are the correct ones
	expectedFilteredDates := []pb.CustomCost{
		{
			ListUnitPrice: 1.0,
		},
		{
			ListUnitPrice: 5,
		},
		{
			ListUnitPrice: 2.45,
		},
	}

	for i, item := range filteredItems {
		assert.Equal(t, expectedFilteredDates[i].ListUnitPrice, item.ListUnitPrice, "Unit price mismatch")

	}
	//assert mapping to CustomCost object

	assert.Equal(t, lineItems[0].GroupName, filteredItems[0].AccountName, "accout name mismatch")
	assert.Equal(t, "Usage", filteredItems[0].ChargeCategory)
	assert.Equal(t, "Usage for 0", filteredItems[0].Description)
	assert.Equal(t, "0", filteredItems[0].ResourceName)
	assert.NotNil(t, filteredItems[0].Id)
	assert.NotNil(t, filteredItems[0].ProviderId)

	assert.InDelta(t, lineItems[0].TotalPriceCents/100, filteredItems[0].BilledCost, 0.01)
	assert.InDelta(t, filteredItems[0].ListCost, lineItems[0].Quantity*lineItems[0].UnitPriceDollars, 0.01)
	assert.Equal(t, lineItems[0].Quantity, filteredItems[0].UsageQuantity)
	assert.Equal(t, filteredItems[0].UsageUnit, lineItems[0].Unit)
}

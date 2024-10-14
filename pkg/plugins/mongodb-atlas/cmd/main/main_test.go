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
	costs, err := GetPendingInvoices("myOrg", mockClient)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(costs))

	//TODO
	// for i, invoice := range costResponse.UsageDetails {
	// 	assert.Equal(t, invoice.InvoiceId, costs[i].Id)
	// 	assert.Equal(t, invoice.OrganizationName, costs[i].AccountName)
	// 	assert.Equal(t, invoice.Service, costs[i].ChargeCategory)
	// 	assert.Equal(t, invoice.UsageAmount, costs[i].BilledCost)
	// }
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

// TODO delete this
func getCostResponseMock() []byte {
	costResponse := atlasplugin.CostResponse{
		UsageDetails: []atlasplugin.Invoice{

			{
				InvoiceId:        "INV003",
				OrganizationId:   "ORG125",
				OrganizationName: "Gamma Inc",
				Service:          "Networking",
				UsageAmount:      50.00,
				UsageDate:        "2024-10-03",
			},
		},
	}

	mockResponseJson, _ := json.Marshal(costResponse)
	return mockResponseJson
}

func TestGetAtlasCostsForWindow(t *testing.T) {
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {

			costResponse := getCostResponseMock()
			if req.Method == http.MethodGet {
				//return costs
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer(costResponse)),
				}, nil
			} else {
				// Define the response that the mock client will return
				mockResponse := atlasplugin.CreateCostExplorerQueryResponse{
					Token: "fake",
				}
				mockResponseJson, _ := json.Marshal(mockResponse)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer(mockResponseJson)),
				}, nil
			}

		},
	}
	atlasCostSource := AtlasCostSource{
		orgID:       "myOrg",
		atlasClient: mockClient,
	}
	// Define the start and end time for the window
	startTime := time.Now().Add(-24 * time.Hour) // 24 hours ago
	endTime := time.Now()                        // Now

	// Create a new Window instance
	window := opencost.NewWindow(&startTime, &endTime)
	resp, error := atlasCostSource.getAtlasCostsForWindow(&window)
	assert.Nil(t, error)
	assert.True(t, resp != nil)
	assert.Equal(t, "data_storage", resp.CostSource)
	assert.Equal(t, "mongodb-atlas", resp.Domain)
	assert.Equal(t, "v1", resp.Version)
	assert.Equal(t, "USD", resp.Currency)
	assert.Equal(t, 1, len(resp.Costs))
}

func TestGetCosts(t *testing.T) {
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {

			costResponse := getCostResponseMock()
			if req.Method == http.MethodGet {
				//return costs
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer(costResponse)),
				}, nil
			} else {
				// Define the response that the mock client will return
				mockResponse := atlasplugin.CreateCostExplorerQueryResponse{
					Token: "fake",
				}
				mockResponseJson, _ := json.Marshal(mockResponse)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer(mockResponseJson)),
				}, nil
			}

		},
	}
	atlasCostSource := AtlasCostSource{
		orgID:       "myOrg",
		atlasClient: mockClient,
	}
	// Define the start and end time for the window
	startTime := time.Now().Add(-24 * time.Hour) // 24 hours ago
	endTime := time.Now()

	customCostRequest := pb.CustomCostRequest{
		Start:      timestamppb.New(startTime),
		End:        timestamppb.New(endTime),
		Resolution: durationpb.New(time.Hour), // Example resolution: 1 hour
	} // Now

	resp := atlasCostSource.GetCustomCosts(&customCostRequest)

	assert.Equal(t, 1, len(resp))

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

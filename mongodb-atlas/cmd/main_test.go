package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	atlasplugin "github.com/opencost/opencost-plugins/mongodb-atlas/plugin"
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

func TestCreateCostExplorerQueryToken(t *testing.T) {
	// Mock data
	org := "testOrg"
	startDate := time.Date(2023, 9, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2023, 9, 30, 0, 0, 0, 0, time.UTC)
	expectedToken := "mockToken"

	// Define the response that the mock client will return
	mockResponse := atlasplugin.CreateCostExplorerQueryResponse{
		Token: expectedToken,
	}
	mockResponseJson, _ := json.Marshal(mockResponse)

	// Create a mock HTTPClient that returns a successful response
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Verify that the request method and URL are correct
			if req.Method != http.MethodPost {
				t.Errorf("expected POST request, got %s", req.Method)
			}
			expectedURL := fmt.Sprintf(costExplorerFmt, org)
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

	// Call the function under test
	token, err := CreateCostExplorerQueryToken(org, startDate, endDate, mockClient)

	// Assert that the function returned the expected token and no error
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if token != expectedToken {
		t.Errorf("expected token %s, got %s", expectedToken, token)
	}
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

func TestErrorFromServer(t *testing.T) {

	// Define the layout that matches the format of the date string
	layout := "2006-01-02"
	endTime, _ := time.Parse(layout, "2024-07-01")
	startTime, _ := time.Parse(layout, "2023-12-01")
	orgId := "1"
	// Create a mock HTTPClient that returns a successful response
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {

			// Return a mock response with status 200 and mock JSON body
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewBufferString("fake")),
			}, nil
		},
	}
	_, err := CreateCostExplorerQueryToken(orgId, startTime, endTime, mockClient)

	assert.NotEmpty(t, err)

}

func TestCallToCreateCostExplorerQueryBadMessage(t *testing.T) {

	// Define the layout that matches the format of the date string
	layout := "2006-01-02"
	endTime, _ := time.Parse(layout, "2024-07-01")
	startTime, _ := time.Parse(layout, "2023-12-01")
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {

			// Return a mock response with status 200 and mock JSON body
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("This ain't json")),
			}, nil
		},
	}

	_, error := CreateCostExplorerQueryToken("myOrg", startTime, endTime, mockClient)
	assert.NotEmpty(t, error)

}

// tests for getCosts
func TestGetCostsMultipleInvoices(t *testing.T) {
	costResponse := atlasplugin.CostResponse{
		UsageDetails: []atlasplugin.Invoice{
			{
				InvoiceId:        "INV001",
				OrganizationId:   "ORG123",
				OrganizationName: "Acme Corp",
				Service:          "Compute",
				UsageAmount:      120.50,
				UsageDate:        "2024-10-01",
			},
			{
				InvoiceId:        "INV002",
				OrganizationId:   "ORG124",
				OrganizationName: "Beta Corp",
				Service:          "Storage",
				UsageAmount:      75.75,
				UsageDate:        "2024-10-02",
			},
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

	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Verify that the request method and URL are correct
			if req.Method != http.MethodGet {
				t.Errorf("expected GET request, got %s", req.Method)
			}
			expectedURL := fmt.Sprintf(costExplorerQueryFmt, "myOrg", "t1")
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
	costs, err := GetCosts(mockClient, "myOrg", "t1")
	assert.Nil(t, err)
	assert.Equal(t, 3, len(costs))

	for i, invoice := range costResponse.UsageDetails {
		assert.Equal(t, invoice.InvoiceId, costs[i].Id)
		assert.Equal(t, invoice.OrganizationName, costs[i].AccountName)
		assert.Equal(t, invoice.Service, costs[i].ChargeCategory)
		assert.Equal(t, invoice.UsageAmount, costs[i].BilledCost)
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
	costs, err := GetCosts(mockClient, "myOrg", "t1")

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

	_, error := GetCosts(mockClient, "myOrg", "t1")
	assert.NotEmpty(t, error)

}

func TestRepeatCallTill200(t *testing.T) {

	var count = 0
	mockResponseJson := getCostResponseMock()

	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			count++

			if count < 2 {
				// Return a mock response with status 200 and mock JSON body
				return &http.Response{
					StatusCode: http.StatusProcessing,
					Body:       io.NopCloser(bytes.NewBuffer(mockResponseJson)),
				}, nil

			} else {
				// Return a mock response with status 200 and mock JSON body
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer(mockResponseJson)),
				}, nil

			}

		},
	}

	costs, err := GetCosts(mockClient, "myOrg", "t1")
	assert.Nil(t, err)
	assert.Equal(t, 1, len(costs))
}

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

func TestStuckInProcessing(t *testing.T) {

	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {

			// Return a mock response with status 200 and mock JSON body
			return &http.Response{
				StatusCode: http.StatusProcessing,
				Body:       io.NopCloser(bytes.NewBufferString("")),
			}, nil

		},
	}

	costs, err := GetCosts(mockClient, "myOrg", "t1")
	assert.NotNil(t, err)
	assert.Nil(t, costs)
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

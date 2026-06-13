package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/icholy/digest"
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
func TestMain(t *testing.T) {
	publicKey := os.Getenv("mapuk")
	privateKey := os.Getenv("maprk")
	orgId := os.Getenv("maorgid")
	if publicKey == "" || privateKey == "" || orgId == "" {
		t.Skip("Skipping integration test.")
	}

	assert.NotNil(t, publicKey)
	assert.NotNil(t, privateKey)
	assert.NotNil(t, orgId)

	client := &http.Client{
		Transport: &digest.Transport{
			Username: publicKey,
			Password: privateKey,
		},
	}

	atlasCostSource := AtlasCostSource{
		orgID:       "myOrg",
		atlasClient: client,
	}
	// Define the start and end time for the window
	now := time.Now()
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	customCostRequest := pb.CustomCostRequest{
		Start:      timestamppb.New(currentMonthStart),                     // Start in current month
		End:        timestamppb.New(currentMonthStart.Add(24 * time.Hour)), // End in current month
		Resolution: durationpb.New(24 * time.Hour),                         // 1 day resolution
	}

	resp := atlasCostSource.GetCustomCosts(&customCostRequest)

	assert.NotEmpty(t, resp)
}

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
			expectedURL := fmt.Sprintf(costExplorerPendingInvoicesURL, "myOrg")
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
		// TODO add more asserts on the fields
	}
}

func TestGetCostErrorFromServer(t *testing.T) {
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Return a mock response with status 200 and mock JSON body
			return nil, fmt.Errorf("mock error: failed to execute request")
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

	_, err := GetPendingInvoices("myOrd", mockClient)
	assert.NotEmpty(t, err)
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
			UnitPriceDollars: 0.02,
		},
	}

	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Verify that the request method and URL are correct
			if req.Method != http.MethodGet {
				t.Errorf("expected GET request, got %s", req.Method)
			}
			expectedURL := fmt.Sprintf(costExplorerPendingInvoicesURL, "myOrg")
			if req.URL.String() != expectedURL {
				t.Errorf("expected URL %s, got %s", expectedURL, req.URL.String())
			}

			// Return a mock response with status 200 and mock JSON body
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("mocked response")),
			}, nil
		},
	}

	// Use the mockClient to test the function
	costs, err := atlasCostSource.GetCustomCosts(nil)

	assert.Nil(t, err)
	assert.NotEmpty(t, costs)
}

func TestFilterInvoicesOnWindow(t *testing.T) {
	now := time.Now()
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	invoices := []atlasplugin.LineItem{
		{
			ClusterName:      "kubecost-mongo-dev-1",
			Created:          currentMonthStart.Add(-1 * time.Hour).Format(time.RFC3339),
			EndDate:          currentMonthStart.Add(24 * time.Hour).Format(time.RFC3339),
			GroupId:          "66d7254246a21a41036ff33e",
			GroupName:        "Project 0",
			Quantity:         0.0555,
			SKU:              "ATLAS_AWS_DATA_TRANSFER_DIFFERENT_REGION",
			StartDate:        currentMonthStart.Format(time.RFC3339),
			TotalPriceCents:  0,
			Unit:             "GB",
			UnitPriceDollars: 0.02,
		},
		{
			ClusterName:      "kubecost-mongo-dev-1",
			Created:          currentMonthStart.Add(48 * time.Hour).Format(time.RFC3339),
			EndDate:          currentMonthStart.Add(72 * time.Hour).Format(time.RFC3339),
			GroupId:          "66d7254246a21a41036ff33e",
			GroupName:        "Project 1",
			Quantity:         0.0555,
			SKU:              "ATLAS_AWS_DATA_TRANSFER_DIFFERENT_REGION",
			StartDate:        currentMonthStart.Add(24 * time.Hour).Format(time.RFC3339),
			TotalPriceCents:  0,
			Unit:             "GB",
			UnitPriceDollars: 0.02,
		},
	}

	startWindow := currentMonthStart
	endWindow := currentMonthStart.Add(24 * time.Hour)

	filtered := FilterInvoicesOnWindow(invoices, startWindow, endWindow)
	assert.Equal(t, 1, len(filtered)) // Expecting 1 line item in the window
}

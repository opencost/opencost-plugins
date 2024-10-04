package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	atlasplugin "github.com/opencost/opencost-plugins/mongodb-atlas/plugin"
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

func TestSanity(t *testing.T) {
	assert.True(t, true)
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
				Body:       ioutil.NopCloser(bytes.NewBuffer(mockResponseJson)),
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
			// Verify that the request method and URL are correct
			if req.Method != http.MethodPost {
				t.Errorf("expected POST request, got %s", req.Method)
			}
			expectedURL := fmt.Sprintf(costExplorerFmt, orgId)
			if req.URL.String() != expectedURL {
				t.Errorf("expected URL %s, got %s", expectedURL, req.URL.String())
			}

			// Return a mock response with status 200 and mock JSON body
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       ioutil.NopCloser(bytes.NewBufferString("fake")),
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
			// Verify that the request method and URL are correct
			if req.Method != http.MethodPost {
				t.Errorf("expected POST request, got %s", req.Method)
			}
			expectedURL := fmt.Sprintf(costExplorerFmt, "myOrg")
			if req.URL.String() != expectedURL {
				t.Errorf("expected URL %s, got %s", expectedURL, req.URL.String())
			}

			// Return a mock response with status 200 and mock JSON body
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       ioutil.NopCloser(bytes.NewBufferString("This ain't json")),
			}, nil
		},
	}

	_, error := CreateCostExplorerQueryToken("myOrg", startTime, endTime, mockClient)
	assert.NotEmpty(t, error)

}

//tests for getCosts

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/icholy/digest"
	"github.com/stretchr/testify/assert"
)

type ClientMock struct {
}

func (c *ClientMock) Do(req *http.Request) (*http.Response, error) {
	// Implement a mock response
	return nil, errors.New("Test Error")
}

func TestCallToCreateCostExplorerQuery(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"Token":"mockToken"}`))
	}))
	defer server.Close()

	mockClient := &http.Client{}
	// Define the layout that matches the format of the date string
	layout := "2006-01-02"
	endTime, _ := time.Parse(layout, "2024-07-01")
	startTime, _ := time.Parse(layout, "2023-12-01")
	resp, error := createCostExplorerQueryToken("myOrg", startTime, endTime, mockClient, server.URL)
	assert.Nil(t, error)
	assert.Equal(t, "mockToken", resp)

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

	// Define the layout that matches the format of the date string
	layout := "2006-01-02"
	endTime, _ := time.Parse(layout, "2024-07-01")
	startTime, _ := time.Parse(layout, "2023-12-01")
	url := "https://cloud.mongodb.com/api/atlas/v2/orgs/" + orgId + "/billing/costExplorer/usage"
	resp, err := createCostExplorerQueryToken(orgId, startTime, endTime, client, url)

	assert.NotEmpty(t, resp)
	assert.Nil(t, err)

}

func TestErrorFromServer(t *testing.T) {

	client := &ClientMock{}
	// Define the layout that matches the format of the date string
	layout := "2006-01-02"
	endTime, _ := time.Parse(layout, "2024-07-01")
	startTime, _ := time.Parse(layout, "2023-12-01")
	orgId := "1"
	url := "https://cloud.mongodb.com/api/atlas/v2/orgs/" + orgId + "/billing/costExplorer/usage"
	_, err := createCostExplorerQueryToken(orgId, startTime, endTime, client, url)

	assert.NotEmpty(t, err)

}

func TestCallToCreateCostExplorerQueryBadMessage(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`this is not json`))
	}))
	defer server.Close()

	mockClient := &http.Client{}
	// Define the layout that matches the format of the date string
	layout := "2006-01-02"
	endTime, _ := time.Parse(layout, "2024-07-01")
	startTime, _ := time.Parse(layout, "2023-12-01")
	_, error := createCostExplorerQueryToken("myOrg", startTime, endTime, mockClient, server.URL)
	assert.NotEmpty(t, error)

}

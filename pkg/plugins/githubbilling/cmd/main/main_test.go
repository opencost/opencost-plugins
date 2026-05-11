package main

import (
	"encoding/json"
	"testing"

	githubbillingplugin "github.com/opencost/opencost-plugins/pkg/plugins/githubbilling/githubbillingplugin"
)

func TestGitHubBillingUsageItemUnmarshal(t *testing.T) {
	jsonData := `{
		"date": "2024-01-15",
		"product": "Actions",
		"sku": "Actions - Ubuntu",
		"quantity": 5000,
		"unitType": "minutes",
		"pricePerUnit": 0.008,
		"grossAmount": 40.0,
		"discountAmount": 10.0,
		"netAmount": 30.0,
		"organizationName": "my-org",
		"repositoryName": "my-repo"
	}`

	var item githubbillingplugin.GitHubBillingUsageItem
	err := json.Unmarshal([]byte(jsonData), &item)
	if err != nil {
		t.Fatalf("Error unmarshalling usage item: %v", err)
	}

	if item.Product != "Actions" {
		t.Errorf("Expected product 'Actions', got '%s'", item.Product)
	}
	if item.Quantity != 5000 {
		t.Errorf("Expected quantity 5000, got %d", item.Quantity)
	}
	if item.NetAmount != 30.0 {
		t.Errorf("Expected net amount 30.0, got %f", item.NetAmount)
	}
	if item.RepositoryName != "my-repo" {
		t.Errorf("Expected repository name 'my-repo', got '%s'", item.RepositoryName)
	}
}

func TestGitHubBillingUsageResponseUnmarshal(t *testing.T) {
	jsonData := `{
		"usageItems": [
			{
				"date": "2024-01-15",
				"product": "Actions",
				"sku": "Actions - Ubuntu",
				"quantity": 5000,
				"unitType": "minutes",
				"pricePerUnit": 0.008,
				"grossAmount": 40.0,
				"discountAmount": 10.0,
				"netAmount": 30.0,
				"organizationName": "my-org",
				"repositoryName": "my-repo"
			},
			{
				"date": "2024-01-15",
				"product": "Packages",
				"sku": "Packages - Storage",
				"quantity": 100,
				"unitType": "GB",
				"pricePerUnit": 0.25,
				"grossAmount": 25.0,
				"discountAmount": 0.0,
				"netAmount": 25.0,
				"organizationName": "my-org"
			}
		]
	}`

	var resp githubbillingplugin.GitHubBillingUsageResponse
	err := json.Unmarshal([]byte(jsonData), &resp)
	if err != nil {
		t.Fatalf("Error unmarshalling usage response: %v", err)
	}

	if len(resp.UsageItems) != 2 {
		t.Errorf("Expected 2 usage items, got %d", len(resp.UsageItems))
	}
	if resp.UsageItems[0].Product != "Actions" {
		t.Errorf("Expected first product 'Actions', got '%s'", resp.UsageItems[0].Product)
	}
	if resp.UsageItems[1].Product != "Packages" {
		t.Errorf("Expected second product 'Packages', got '%s'", resp.UsageItems[1].Product)
	}
}

func TestGitHubBillingPremiumUsageItemUnmarshal(t *testing.T) {
	jsonData := `{
		"product": "Copilot",
		"sku": "copilot-business",
		"model": "gpt-4",
		"unitType": "requests",
		"pricePerUnit": 0.01,
		"grossQuantity": 1000,
		"grossAmount": 10.0,
		"discountQuantity": 200,
		"discountAmount": 2.0,
		"netQuantity": 800,
		"netAmount": 8.0
	}`

	var item githubbillingplugin.GitHubBillingPremiumUsageItem
	err := json.Unmarshal([]byte(jsonData), &item)
	if err != nil {
		t.Fatalf("Error unmarshalling premium usage item: %v", err)
	}

	if item.Product != "Copilot" {
		t.Errorf("Expected product 'Copilot', got '%s'", item.Product)
	}
	if item.Model != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got '%s'", item.Model)
	}
	if item.NetAmount != 8.0 {
		t.Errorf("Expected net amount 8.0, got %f", item.NetAmount)
	}
}

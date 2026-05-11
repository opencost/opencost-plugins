package main

import (
	"encoding/json"
	"testing"

	confluentplugin "github.com/opencost/opencost-plugins/pkg/plugins/confluent/confluentplugin"
)

func TestConfluentCostUnmarshal(t *testing.T) {
	jsonData := `{
		"api_version": "billing/v1",
		"kind": "Cost",
		"id": "abc-123",
		"start_date": "2024-01-01",
		"end_date": "2024-01-31",
		"granularity": "MONTHLY",
		"line_type": "CHARGE",
		"product": "Kafka",
		"network_access_type": "PRIVATE",
		"quantity": 100.5,
		"unit": "GB",
		"price": 0.11,
		"amount": 11.055,
		"original_amount": 13.0,
		"discount_amount": 1.945,
		"resource": {
			"id": "lkc-abc123",
			"display_name": "my-kafka-cluster",
			"environment": {
				"id": "env-xyz789"
			}
		}
	}`

	var cost confluentplugin.ConfluentCost
	err := json.Unmarshal([]byte(jsonData), &cost)
	if err != nil {
		t.Fatalf("Error unmarshalling cost: %v", err)
	}

	if cost.Product != "Kafka" {
		t.Errorf("Expected product 'Kafka', got '%s'", cost.Product)
	}
	if cost.Amount != 11.055 {
		t.Errorf("Expected amount 11.055, got %f", cost.Amount)
	}
	if cost.Quantity != 100.5 {
		t.Errorf("Expected quantity 100.5, got %f", cost.Quantity)
	}
	if cost.Resource.DisplayName != "my-kafka-cluster" {
		t.Errorf("Expected resource display name 'my-kafka-cluster', got '%s'", cost.Resource.DisplayName)
	}
	if cost.Resource.Environment.ID != "env-xyz789" {
		t.Errorf("Expected environment ID 'env-xyz789', got '%s'", cost.Resource.Environment.ID)
	}
	if cost.NetworkAccessType != "PRIVATE" {
		t.Errorf("Expected network access type 'PRIVATE', got '%s'", cost.NetworkAccessType)
	}
	if cost.DiscountAmount != 1.945 {
		t.Errorf("Expected discount amount 1.945, got %f", cost.DiscountAmount)
	}
}

func TestConfluentCostListUnmarshal(t *testing.T) {
	jsonData := `{
		"api_version": "billing/v1",
		"kind": "CostList",
		"metadata": {
			"total_size": 2,
			"page_size": 100,
			"page_token": "token-abc",
			"next_page_token": ""
		},
		"data": [
			{
				"api_version": "billing/v1",
				"kind": "Cost",
				"id": "cost-1",
				"start_date": "2024-01-01",
				"end_date": "2024-01-31",
				"product": "Kafka",
				"quantity": 100.0,
				"unit": "GB",
				"price": 0.11,
				"amount": 11.0,
				"resource": {
					"id": "lkc-abc123",
					"display_name": "my-kafka-cluster",
					"environment": {"id": "env-xyz789"}
				}
			},
			{
				"api_version": "billing/v1",
				"kind": "Cost",
				"id": "cost-2",
				"start_date": "2024-01-01",
				"end_date": "2024-01-31",
				"product": "Schema Registry",
				"quantity": 50.0,
				"unit": "GB",
				"price": 0.05,
				"amount": 2.5,
				"resource": {
					"id": "lsrc-def456",
					"display_name": "my-schema-registry",
					"environment": {"id": "env-xyz789"}
				}
			}
		]
	}`

	var costList confluentplugin.ConfluentCostList
	err := json.Unmarshal([]byte(jsonData), &costList)
	if err != nil {
		t.Fatalf("Error unmarshalling cost list: %v", err)
	}

	if len(costList.Data) != 2 {
		t.Errorf("Expected 2 cost entries, got %d", len(costList.Data))
	}

	if costList.Data[0].Product != "Kafka" {
		t.Errorf("Expected first product 'Kafka', got '%s'", costList.Data[0].Product)
	}

	if costList.Data[1].Product != "Schema Registry" {
		t.Errorf("Expected second product 'Schema Registry', got '%s'", costList.Data[1].Product)
	}

	if costList.Metadata.TotalSize != 2 {
		t.Errorf("Expected total_size 2, got %d", costList.Metadata.TotalSize)
	}
}

package main

import (
	"encoding/json"
	"testing"

	cloudamqpplugin "github.com/opencost/opencost-plugins/pkg/plugins/cloudamqp/cloudamqpplugin"
)

func TestCloudAMQPInstanceUnmarshal(t *testing.T) {
	jsonData := `{
		"id": 12345,
		"name": "my-rabbitmq",
		"plan": "bunny-1",
		"plan_id": 1,
		"region": "amazon-web-services::us-east-1",
		"nodes": 1,
		"node_type": "t3.micro",
		"ready": true,
		"price": {
			"amount": 29.0,
			"currency": "USD",
			"interval": "monthly"
		}
	}`

	var instance cloudamqpplugin.CloudAMQPInstance
	err := json.Unmarshal([]byte(jsonData), &instance)
	if err != nil {
		t.Fatalf("Error unmarshalling instance: %v", err)
	}

	if instance.Name != "my-rabbitmq" {
		t.Errorf("Expected name 'my-rabbitmq', got '%s'", instance.Name)
	}
	if instance.Plan != "bunny-1" {
		t.Errorf("Expected plan 'bunny-1', got '%s'", instance.Plan)
	}
	if instance.Price.Amount != 29.0 {
		t.Errorf("Expected price amount 29.0, got %f", instance.Price.Amount)
	}
	if instance.Price.Currency != "USD" {
		t.Errorf("Expected currency 'USD', got '%s'", instance.Price.Currency)
	}
	if instance.Nodes != 1 {
		t.Errorf("Expected 1 node, got %d", instance.Nodes)
	}
}

func TestCloudAMQPInstancesListUnmarshal(t *testing.T) {
	jsonData := `[
		{
			"id": 12345,
			"name": "my-rabbitmq",
			"plan": "bunny-1",
			"region": "amazon-web-services::us-east-1",
			"price": {"amount": 29.0, "currency": "USD", "interval": "monthly"}
		},
		{
			"id": 67890,
			"name": "my-rabbitmq-2",
			"plan": "panda-3",
			"region": "amazon-web-services::eu-west-1",
			"price": {"amount": 199.0, "currency": "USD", "interval": "monthly"}
		}
	]`

	var instances []cloudamqpplugin.CloudAMQPInstance
	err := json.Unmarshal([]byte(jsonData), &instances)
	if err != nil {
		t.Fatalf("Error unmarshalling instances: %v", err)
	}

	if len(instances) != 2 {
		t.Errorf("Expected 2 instances, got %d", len(instances))
	}
	if instances[1].Plan != "panda-3" {
		t.Errorf("Expected second plan 'panda-3', got '%s'", instances[1].Plan)
	}
}

func TestCloudAMQPInvoiceUnmarshal(t *testing.T) {
	jsonData := `{
		"id": 1001,
		"amount": 228.0,
		"currency": "USD",
		"description": "Monthly subscription",
		"period_start": "2024-01-01",
		"period_end": "2024-01-31",
		"status": "paid",
		"items": [
			{
				"description": "bunny-1 plan",
				"amount": 29.0,
				"quantity": 1,
				"instance_id": 12345,
				"instance_name": "my-rabbitmq",
				"plan": "bunny-1"
			}
		]
	}`

	var invoice cloudamqpplugin.CloudAMQPInvoice
	err := json.Unmarshal([]byte(jsonData), &invoice)
	if err != nil {
		t.Fatalf("Error unmarshalling invoice: %v", err)
	}

	if invoice.Amount != 228.0 {
		t.Errorf("Expected amount 228.0, got %f", invoice.Amount)
	}
	if len(invoice.Items) != 1 {
		t.Errorf("Expected 1 item, got %d", len(invoice.Items))
	}
	if invoice.Items[0].Plan != "bunny-1" {
		t.Errorf("Expected plan 'bunny-1', got '%s'", invoice.Items[0].Plan)
	}
}

package main

import (
	"testing"

	cloudamqpplugin "github.com/opencost/opencost-plugins/pkg/plugins/cloudamqp/cloudamqpplugin"
	"golang.org/x/time/rate"
)

func TestMakeInstanceMap(t *testing.T) {
	instances := []cloudamqpplugin.CloudAMQPInstance{
		{ID: 1, Name: "instance-1", Plan: "bunny-1", Region: "amazon-web-services::us-east-1"},
		{ID: 2, Name: "instance-2", Plan: "panda-1", Region: "amazon-web-services::eu-west-1"},
	}

	m := makeInstanceMap(instances)

	if len(m) != 2 {
		t.Fatalf("expected 2 instances in map, got %d", len(m))
	}

	if m[1].Name != "instance-1" {
		t.Errorf("expected instance-1, got %s", m[1].Name)
	}

	if m[2].Region != "amazon-web-services::eu-west-1" {
		t.Errorf("expected region amazon-web-services::eu-west-1, got %s", m[2].Region)
	}
}

func TestCloudAMQPCostSourceCreation(t *testing.T) {
	costSrc := CloudAMQPCostSource{
		apiKey:      "test-key",
		accountID:   "test-account",
		rateLimiter: rate.NewLimiter(1.0, 2),
		httpClient:  nil, // will be set in production
	}

	if costSrc.apiKey != "test-key" {
		t.Errorf("expected apiKey 'test-key', got %s", costSrc.apiKey)
	}

	if costSrc.accountID != "test-account" {
		t.Errorf("expected accountID 'test-account', got %s", costSrc.accountID)
	}
}

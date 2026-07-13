package main

import (
	"testing"

	cloudflareplugin "github.com/opencost/opencost-plugins/pkg/plugins/cloudflare/cloudflareplugin"
	"golang.org/x/time/rate"
)

func TestMakeZoneMap(t *testing.T) {
	zones := []cloudflareplugin.CloudflareZone{
		{ID: "zone-1", Name: "example.com", Status: "active", Plan: cloudflareplugin.CloudflareZonePlan{Name: "Pro"}},
		{ID: "zone-2", Name: "test.org", Status: "active", Plan: cloudflareplugin.CloudflareZonePlan{Name: "Business"}},
	}

	m := makeZoneMap(zones)

	if len(m) != 2 {
		t.Fatalf("expected 2 zones in map, got %d", len(m))
	}

	if m["zone-1"].Name != "example.com" {
		t.Errorf("expected example.com, got %s", m["zone-1"].Name)
	}

	if m["zone-2"].Plan.Name != "Business" {
		t.Errorf("expected Business plan, got %s", m["zone-2"].Plan.Name)
	}
}

func TestCloudflareCostSourceCreation(t *testing.T) {
	costSrc := CloudflareCostSource{
		apiKey:      "test-key",
		email:       "test@example.com",
		accountID:   "test-account",
		rateLimiter: rate.NewLimiter(2.0, 5),
		httpClient:  nil, // will be set in production
	}

	if costSrc.apiKey != "test-key" {
		t.Errorf("expected apiKey 'test-key', got %s", costSrc.apiKey)
	}

	if costSrc.email != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got %s", costSrc.email)
	}

	if costSrc.accountID != "test-account" {
		t.Errorf("expected accountID 'test-account', got %s", costSrc.accountID)
	}
}

func TestParseIntOrDefault(t *testing.T) {
	tests := []struct {
		input    string
		def      int
		expected int
	}{
		{"42", 0, 42},
		{"0", 10, 0},
		{"", 10, 10},
		{"abc", 10, 10},
	}

	for _, tt := range tests {
		result := parseIntOrDefault(tt.input, tt.def)
		if result != tt.expected {
			t.Errorf("parseIntOrDefault(%q, %d) = %d, expected %d", tt.input, tt.def, result, tt.expected)
		}
	}
}

func TestMakeZoneMapEmpty(t *testing.T) {
	m := makeZoneMap([]cloudflareplugin.CloudflareZone{})

	if len(m) != 0 {
		t.Errorf("expected empty map, got %d entries", len(m))
	}
}

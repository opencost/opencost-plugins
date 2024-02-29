package main

import (
	"fmt"
	"testing"
)

func TestPricingFetch(t *testing.T) {
	listPricing, err := scrapeDatadogPrices(url)
	if err != nil {
		t.Fatalf("failed to get pricing: %v", err)
	}
	fmt.Printf("got response: %v", listPricing)
	if len(listPricing.Details) == 0 {
		t.Fatalf("expected non zero pricing details")
	}
}

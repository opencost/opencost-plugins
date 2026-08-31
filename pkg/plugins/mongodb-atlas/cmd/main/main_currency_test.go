package main

import (
	"testing"
	"time"

	atlasplugin "github.com/opencost/opencost-plugins/pkg/plugins/mongodb-atlas/plugin"
	"github.com/opencost/opencost/core/pkg/opencost"
	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

type MockConverter struct {
	rate float64
}

func (m *MockConverter) Convert(amount float64, from, to string) (float64, error) {
	if from == "USD" && to == "EUR" {
		return amount * m.rate, nil
	}
	return amount, nil
}

func (m *MockConverter) GetRate(from, to string) (float64, error) {
	if from == "USD" && to == "EUR" {
		return m.rate, nil
	}
	return 1.0, nil
}

func (m *MockConverter) GetSupportedCurrencies() ([]string, error) {
	return []string{"USD", "EUR", "GBP"}, nil
}

func TestAtlasCostSource_CurrencyConversion(t *testing.T) {
	mockConverter := &MockConverter{rate: 0.85}
	
	costSource := &AtlasCostSource{
		orgID:             "test-org",
		rateLimiter:       rate.NewLimiter(1.0, 1),
		targetCurrency:    "EUR",
		currencyConverter: mockConverter,
	}
	
	start := time.Now().Add(-24 * time.Hour).UTC()
	end := time.Now().UTC()
	win := opencost.NewWindow(&start, &end)
	
	// Create test line items with USD prices
	// Note: StartDate and EndDate need to be within the window to be included
	itemStart := start.Add(1 * time.Hour)
	itemEnd := end.Add(-1 * time.Hour)
	lineItems := []atlasplugin.LineItem{
		{
			ClusterName:      "test-cluster",
			GroupId:          "test-group",
			GroupName:        "Test Group",
			SKU:              "TEST_SKU",
			TotalPriceCents:  10000, // $100.00
			UnitPriceDollars: 1.0,
			Quantity:         100,
			Unit:             "hours",
			StartDate:        itemStart.Format(time.RFC3339),
			EndDate:          itemEnd.Format(time.RFC3339),
		},
	}
	
	resp := costSource.getAtlasCostsForWindow(&win, lineItems)
	
	assert.Equal(t, "EUR", resp.Currency)
	assert.Len(t, resp.Costs, 1)
	
	cost := resp.Costs[0]
	expectedBilledCost := float32(100.0 * 0.85)
	expectedListCost := float32(100.0 * 0.85)
	expectedUnitPrice := float32(1.0 * 0.85)
	
	assert.Equal(t, expectedBilledCost, cost.BilledCost)
	assert.Equal(t, expectedListCost, cost.ListCost)
	assert.Equal(t, expectedUnitPrice, cost.ListUnitPrice)
}

func TestAtlasCostSource_NoConversion(t *testing.T) {
	costSource := &AtlasCostSource{
		orgID:          "test-org",
		rateLimiter:    rate.NewLimiter(1.0, 1),
		targetCurrency: "USD",
	}
	
	start := time.Now().Add(-24 * time.Hour).UTC()
	end := time.Now().UTC()
	win := opencost.NewWindow(&start, &end)
	
	itemStart := start.Add(1 * time.Hour)
	itemEnd := end.Add(-1 * time.Hour)
	lineItems := []atlasplugin.LineItem{
		{
			ClusterName:      "test-cluster",
			GroupId:          "test-group",
			GroupName:        "Test Group",
			SKU:              "TEST_SKU",
			TotalPriceCents:  10000, // $100.00
			UnitPriceDollars: 1.0,
			Quantity:         100,
			Unit:             "hours",
			StartDate:        itemStart.Format(time.RFC3339),
			EndDate:          itemEnd.Format(time.RFC3339),
		},
	}
	
	resp := costSource.getAtlasCostsForWindow(&win, lineItems)
	
	assert.Equal(t, "USD", resp.Currency)
	assert.Len(t, resp.Costs, 1)
	
	cost := resp.Costs[0]
	assert.Equal(t, float32(100.0), cost.BilledCost)
	assert.Equal(t, float32(100.0), cost.ListCost)
	assert.Equal(t, float32(1.0), cost.ListUnitPrice)
}
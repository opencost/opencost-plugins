//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/opencost/opencost/core/pkg/model/pb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func main() {
	// Create mock daily response
	now := time.Now()
	dailyStart := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, time.UTC)
	dailyEnd := dailyStart.AddDate(0, 0, 1)

	dailyResponse := &pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v3"},
		CostSource: "billing",
		Domain:     "fastly",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(dailyStart),
		End:        timestamppb.New(dailyEnd),
		Errors:     []string{},
		Costs: []*pb.CustomCost{
			{
				Zone:           "North America",
				AccountName:    "test-customer-123",
				ChargeCategory: "usage",
				Description:    "CDN Bandwidth Usage",
				ResourceName:   "bandwidth",
				ResourceType:   "Full Site Delivery",
				Id:             "inv-test-001",
				ProviderId:     "inv-test-001/Full Site Delivery/bandwidth",
				Labels: map[string]string{
					"product_line": "CDN",
					"product_name": "Full Site Delivery",
				},
				ListCost:      75.50,
				ListUnitPrice: 0.12,
				BilledCost:    75.50,
				UsageQuantity: 629.17,
				UsageUnit:     "GB",
			},
			{
				Zone:           "Global",
				AccountName:    "test-customer-123",
				ChargeCategory: "usage",
				Description:    "Edge Compute Usage",
				ResourceName:   "compute_time",
				ResourceType:   "Compute",
				Id:             "inv-test-001",
				ProviderId:     "inv-test-001/Compute@Edge/compute_time",
				Labels: map[string]string{
					"product_line": "Edge Compute",
					"product_name": "Compute@Edge",
				},
				ListCost:      50.00,
				ListUnitPrice: 1.00,
				BilledCost:    50.00,
				UsageQuantity: 50.0,
				UsageUnit:     "hours",
			},
		},
	}

	// Create mock hourly response (smaller amounts)
	hourlyStart := dailyStart
	hourlyEnd := hourlyStart.Add(time.Hour)

	hourlyResponse := &pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v3"},
		CostSource: "billing",
		Domain:     "fastly",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(hourlyStart),
		End:        timestamppb.New(hourlyEnd),
		Errors:     []string{},
		Costs: []*pb.CustomCost{
			{
				Zone:           "North America",
				AccountName:    "test-customer-123",
				ChargeCategory: "usage",
				Description:    "CDN Bandwidth Usage - Hourly",
				ResourceName:   "bandwidth",
				ResourceType:   "Full Site Delivery",
				Id:             "inv-test-hourly-001",
				ProviderId:     "inv-test-hourly-001/Full Site Delivery/bandwidth",
				Labels: map[string]string{
					"product_line": "CDN",
					"product_name": "Full Site Delivery",
				},
				ListCost:      3.15,
				ListUnitPrice: 0.12,
				BilledCost:    3.15,
				UsageQuantity: 26.25,
				UsageUnit:     "GB",
			},
		},
	}

	// Write daily test data
	dailyFile := "fastly_daily_test.json"
	// Create array as expected by validator
	responses := []*pb.CustomCostResponse{dailyResponse}
	// Marshal each response individually and create JSON array
	var responseArray []json.RawMessage
	for _, resp := range responses {
		protoData, _ := protojson.Marshal(resp)
		responseArray = append(responseArray, json.RawMessage(protoData))
	}
	dailyData, _ := json.MarshalIndent(responseArray, "", "  ")
	err := os.WriteFile(dailyFile, dailyData, 0644)
	if err != nil {
		fmt.Printf("Error writing daily file: %v\n", err)
		return
	}

	// Write hourly test data
	hourlyFile := "fastly_hourly_test.json"
	// Create array as expected by validator
	hourlyResponses := []*pb.CustomCostResponse{hourlyResponse}
	// Marshal each response individually and create JSON array
	var hourlyResponseArray []json.RawMessage
	for _, resp := range hourlyResponses {
		protoData, _ := protojson.Marshal(resp)
		hourlyResponseArray = append(hourlyResponseArray, json.RawMessage(protoData))
	}
	hourlyData, _ := json.MarshalIndent(hourlyResponseArray, "", "  ")
	err = os.WriteFile(hourlyFile, hourlyData, 0644)
	if err != nil {
		fmt.Printf("Error writing hourly file: %v\n", err)
		return
	}

	fmt.Printf("Test data files created successfully:\n")
	fmt.Printf("Daily: %s\n", dailyFile)
	fmt.Printf("Hourly: %s\n", hourlyFile)
	fmt.Printf("\nTo test the validator, run:\n")
	fmt.Printf("go run ../cmd/validator/main/main.go %s %s\n", dailyFile, hourlyFile)
}

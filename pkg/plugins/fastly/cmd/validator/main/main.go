package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"google.golang.org/protobuf/encoding/protojson"
)

// the validator is designed to allow plugin implementors to validate their plugin information
// as called by the central test harness.
// this avoids having to ask folks to re-implement the test harness over again for each plugin

// the integration test harness provides a path to a protobuf file for each window
// the validator can then read that in and further validate the response data
// using the domain knowledge of each plugin author
func main() {
	// first arg is the path to the daily protobuf file
	if len(os.Args) < 3 {
		fmt.Println("Usage: validator <path-to-daily-protobuf-file> <path-to-hourly-protobuf-file>")
		os.Exit(1)
	}

	dailyProtobufFilePath := os.Args[1]

	// read in the protobuf file
	data, err := os.ReadFile(dailyProtobufFilePath)
	if err != nil {
		fmt.Printf("Error reading daily protobuf file: %v\n", err)
		os.Exit(1)
	}

	dailyCustomCostResponses, err := Unmarshal(data)
	if err != nil {
		fmt.Printf("Error unmarshalling daily protobuf data: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully unmarshalled %d daily custom cost responses\n", len(dailyCustomCostResponses))

	// second arg is the path to the hourly protobuf file
	hourlyProtobufFilePath := os.Args[2]

	data, err = os.ReadFile(hourlyProtobufFilePath)
	if err != nil {
		fmt.Printf("Error reading hourly protobuf file: %v\n", err)
		os.Exit(1)
	}

	// read in the protobuf file
	hourlyCustomCostResponses, err := Unmarshal(data)
	if err != nil {
		fmt.Printf("Error unmarshalling hourly protobuf data: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully unmarshalled %d hourly custom cost responses\n", len(hourlyCustomCostResponses))

	// validate the custom cost response data
	isvalid := validate(dailyCustomCostResponses, hourlyCustomCostResponses)
	if !isvalid {
		os.Exit(1)
	} else {
		fmt.Println("Validation successful")
	}
}

func validate(respDaily, respHourly []*pb.CustomCostResponse) bool {
	if len(respDaily) == 0 {
		log.Errorf("no daily response received from fastly plugin")
		return false
	}

	if len(respHourly) == 0 {
		log.Errorf("no hourly response received from fastly plugin")
		return false
	}

	var multiErr error

	// parse the response and look for errors
	for _, resp := range respDaily {
		if len(resp.Errors) > 0 {
			multiErr = multierror.Append(multiErr, fmt.Errorf("errors occurred in daily response: %v", resp.Errors))
		}
	}

	for _, resp := range respHourly {
		if resp.Errors != nil {
			multiErr = multierror.Append(multiErr, fmt.Errorf("errors occurred in hourly response: %v", resp.Errors))
		}
	}

	// check if any errors occurred
	if multiErr != nil {
		log.Errorf("Errors occurred during plugin testing for fastly: %v", multiErr)
		return false
	}

	seenResourceTypes := map[string]bool{}
	seenProductGroups := map[string]bool{}
	totalDailyCost := float32(0.0)

	// verify that the returned costs are non zero
	for _, resp := range respDaily {
		if len(resp.Costs) == 0 && resp.Start.AsTime().After(time.Now().Truncate(24*time.Hour).Add(-1*time.Minute)) {
			log.Debugf("today's daily costs returned by fastly plugin are empty, skipping: %v", resp)
			continue
		}

		for _, cost := range resp.Costs {
			totalDailyCost += cost.GetBilledCost()
			seenResourceTypes[cost.GetResourceType()] = true
			seenProductGroups[cost.GetResourceType()] = true

			if cost.GetBilledCost() == 0 {
				log.Debugf("got zero cost for %v", cost)
			}

			// Sanity check - individual line items shouldn't be extremely high
			if cost.GetBilledCost() > 100000 {
				log.Errorf("daily cost returned by fastly plugin for %v is greater than 100,000", cost)
				return false
			}
		}
	}

	// Fastly should have some costs unless it's a free/developer account
	if totalDailyCost == 0 {
		log.Warnf("daily costs returned by fastly plugin are zero - this might be expected for developer accounts")
	}

	// Check that we see some expected product groups
	expectedProductGroups := []string{
		"Full Site Delivery",
		"Compute",
		"Security",
		"Network Services",
	}

	foundAnyExpected := false
	for _, expected := range expectedProductGroups {
		if seenProductGroups[expected] {
			foundAnyExpected = true
			break
		}
	}

	if len(seenProductGroups) > 0 && !foundAnyExpected {
		log.Warnf("none of the expected product groups found in fastly response. Seen product groups: %v", seenProductGroups)
	}

	// verify the domain matches the plugin name
	for _, resp := range respDaily {
		if resp.Domain != "fastly" {
			log.Errorf("daily domain returned by fastly plugin does not match plugin name")
			return false
		}
	}

	// Check hourly responses
	totalHourlyCost := float32(0.0)
	for _, resp := range respHourly {
		for _, cost := range resp.Costs {
			totalHourlyCost += cost.GetBilledCost()
			if cost.GetBilledCost() > 10000 {
				log.Errorf("hourly cost returned by fastly plugin for %v is greater than 10,000", cost)
				return false
			}
		}
	}

	if totalHourlyCost == 0 {
		log.Warnf("hourly costs returned by fastly plugin are zero - this might be expected for developer accounts")
	}

	// Verify currency is USD
	for _, resp := range respDaily {
		if resp.Currency != "USD" {
			log.Errorf("expected currency USD, got %s", resp.Currency)
			return false
		}
	}

	// Verify cost source
	for _, resp := range respDaily {
		if resp.CostSource != "billing" {
			log.Errorf("expected cost source 'billing', got %s", resp.CostSource)
			return false
		}
	}

	return true
}

func Unmarshal(data []byte) ([]*pb.CustomCostResponse, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	protoResps := make([]*pb.CustomCostResponse, len(raw))
	for i, r := range raw {
		p := &pb.CustomCostResponse{}
		if err := protojson.Unmarshal(r, p); err != nil {
			return nil, err
		}
		protoResps[i] = p
	}

	return protoResps, nil
}

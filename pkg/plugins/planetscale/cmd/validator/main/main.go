package main

import (
    "encoding/json"
    "fmt"
    "os"
    "strings"

    "github.com/hashicorp/go-multierror"
    "github.com/opencost/opencost/core/pkg/log"
    "github.com/opencost/opencost/core/pkg/model/pb"
    "google.golang.org/protobuf/encoding/protojson"
)

// The validator is designed to allow plugin implementors to validate their plugin information
// as called by the central test harness.
// This avoids having to ask folks to re-implement the test harness over again for each plugin.

func main() {
	// First arg is the path to the daily protobuf file
	if len(os.Args) < 3 {
		fmt.Println("Usage: validator <path-to-daily-protobuf-file> <path-to-hourly-protobuf-file>")
		os.Exit(1)
	}

	dailyProtobufFilePath := os.Args[1]

	// Read in the daily protobuf file
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

	// Second arg is the path to the hourly protobuf file
	hourlyProtobufFilePath := os.Args[2]

	data, err = os.ReadFile(hourlyProtobufFilePath)
	if err != nil {
		fmt.Printf("Error reading hourly protobuf file: %v\n", err)
		os.Exit(1)
	}

	// Read in the hourly protobuf file
	hourlyCustomCostResponses, err := Unmarshal(data)
	if err != nil {
		fmt.Printf("Error unmarshalling hourly protobuf data: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully unmarshalled %d hourly custom cost responses\n", len(hourlyCustomCostResponses))

	// Validate the custom cost response data
	isValid := validate(dailyCustomCostResponses, hourlyCustomCostResponses)
	if !isValid {
		os.Exit(1)
	} else {
		fmt.Println("Validation successful")
	}
}

func validate(respDaily, respHourly []*pb.CustomCostResponse) bool {
	if len(respDaily) == 0 {
		log.Errorf("No daily response received from planetscale plugin")
		return false
	}

	if len(respHourly) == 0 {
		log.Errorf("No hourly response received from planetscale plugin")
		return false
	}

	var multiErr error

	// Parse the response and look for errors
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

	// Check if any errors occurred
	if multiErr != nil {
		log.Errorf("Errors occurred during plugin testing for planetscale: %v", multiErr)
		return false
	}

	seenCosts := map[string]bool{}
	nonZeroBilledCosts := 0
	// Verify that the returned costs are non-zero
	for _, resp := range respDaily {
		for _, cost := range resp.Costs {
			seenCosts[cost.GetResourceName()] = true
			if !strings.Contains(cost.GetResourceName(), "FREE") && cost.GetListCost() == 0 {
				log.Errorf("Daily list cost returned by plugin planetscale is zero for cost: %v", cost)
				return false
			}
			if cost.GetListCost() >= 0.01 && !strings.Contains(cost.GetResourceName(), "FREE") && cost.GetBilledCost() == 0 {
				log.Errorf("Daily billed cost returned by plugin planetscale is zero for cost: %v", cost)
				return false
			}
			if cost.GetBilledCost() > 0 {
				nonZeroBilledCosts++
			}
		}
	}

	if nonZeroBilledCosts == 0 {
		log.Errorf("No non-zero billed costs returned by plugin planetscale")
		return false
	}

	expectedCosts := []string{
		"PLANETSCALE_DATA_STORAGE",
		"PLANETSCALE_DATA_TRANSFER",
		"PLANETSCALE_INSTANCE",
	}

	for _, cost := range expectedCosts {
		if !seenCosts[cost] {
			log.Errorf("Hourly cost %s not found in plugin planetscale response", cost)
			return false
		}
	}

	if len(seenCosts) != len(expectedCosts) {
		log.Errorf("Hourly costs returned by plugin planetscale do not equal expected costs")
		log.Errorf("Seen costs: %v", seenCosts)
		log.Errorf("Expected costs: %v", expectedCosts)

		log.Errorf("Response: %v", respHourly)
		return false
	}

	// Verify the domain matches the plugin name
	for _, resp := range respDaily {
		if resp.Domain != "planetscale" {
			log.Errorf("Daily domain returned by plugin planetscale does not match plugin name")
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

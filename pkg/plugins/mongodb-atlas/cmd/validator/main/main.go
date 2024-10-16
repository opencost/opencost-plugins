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
		log.Errorf("no daily response received from mongodb-atlas plugin")
		return false
	}

	if len(respHourly) == 0 {
		log.Errorf("no hourly response received from mongodb-atlas plugin")
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
		log.Errorf("Errors occurred during plugin testing for mongodb-atlas: %v", multiErr)
		return false
	}

	seenCosts := map[string]bool{}
	nonZeroBilledCosts := 0
	//verify that the returned costs are non zero
	for _, resp := range respDaily {
		for _, cost := range resp.Costs {
			seenCosts[cost.GetResourceName()] = true
			if !strings.Contains(cost.GetResourceName(), "FREE") && cost.GetListCost() == 0 {
				log.Errorf("daily list cost returned by plugin mongodb-atlas is zero for cost: %v", cost)
				return false
			}
			if cost.GetListCost() >= 0.01 && !strings.Contains(cost.GetResourceName(), "FREE") && cost.GetBilledCost() == 0 {
				log.Errorf("daily billed cost returned by plugin mongodb-atlas is zero for cost: %v", cost)
				return false
			}
			if cost.GetBilledCost() > 0 {
				nonZeroBilledCosts++
			}
		}
	}

	if nonZeroBilledCosts == 0 {
		log.Errorf("no non-zero billed costs returned by plugin mongodb-atlas")
		return false
	}
	expectedCosts := []string{
		"ATLAS_AWS_DATA_TRANSFER_DIFFERENT_REGION",
		"ATLAS_AWS_DATA_TRANSFER_INTERNET",
		"ATLAS_AWS_DATA_TRANSFER_SAME_REGION",
		"ATLAS_AWS_INSTANCE_M10",
		"ATLAS_NDS_AWS_PIT_RESTORE_STORAGE",
		"ATLAS_NDS_AWS_PIT_RESTORE_STORAGE_FREE_TIER",
	}

	for _, cost := range expectedCosts {
		if !seenCosts[cost] {
			log.Errorf("hourly cost %s not found in plugin mongodb-atlas response", cost)
			return false
		}
	}

	if len(seenCosts) != len(expectedCosts) {
		log.Errorf("hourly costs returned by plugin mongodb-atlas do not equal expected costs")
		log.Errorf("seen costs: %v", seenCosts)
		log.Errorf("expected costs: %v", expectedCosts)

		log.Errorf("response: %v", respHourly)
		return false
	}

	// verify the domain matches the plugin name
	for _, resp := range respDaily {
		if resp.Domain != "mongodb-atlas" {
			log.Errorf("daily domain returned by plugin mongodb-atlas does not match plugin name")
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

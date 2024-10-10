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
		log.Errorf("no daily response received from openai plugin")
		return false
	}

	var multiErr error

	// parse the response and look for errors
	for _, resp := range respDaily {
		if len(resp.Errors) > 0 {
			multiErr = multierror.Append(multiErr, fmt.Errorf("errors occurred in daily response: %v", resp.Errors))
		}
	}

	// check if any errors occurred
	if multiErr != nil {
		log.Errorf("Errors occurred during plugin testing for open ai: %v", multiErr)
		return false
	}
	seenCosts := map[string]bool{}
	var costSum float32
	//verify that the returned costs are non zero
	for _, resp := range respDaily {
		if len(resp.Costs) == 0 && resp.Start.AsTime().After(time.Now().Truncate(24*time.Hour).Add(-1*time.Minute)) {
			log.Debugf("today's daily costs returned by plugin openai are empty, skipping: %v", resp)
			continue
		}

		for _, cost := range resp.Costs {
			costSum += cost.GetBilledCost()
			seenCosts[cost.GetResourceName()] = true
			if cost.GetBilledCost() == 0 {
				log.Debugf("got zero cost for %v", cost)
			}
			if cost.GetBilledCost() > 1 {
				log.Errorf("daily cost returned by plugin openai for %v is greater than 1", cost)
				return false
			}
		}

	}
	if costSum == 0 {
		log.Errorf("daily costs returned by openai plugin are zero")
		return false
	}
	expectedCosts := []string{
		"GPT-4o mini",
		"GPT-4o",
		"Other models",
	}

	for _, cost := range expectedCosts {
		if !seenCosts[cost] {
			log.Errorf("daily cost %s not found in plugin openai response", cost)
			return false
		}
	}

	// verify the domain matches the plugin name
	for _, resp := range respDaily {
		if resp.Domain != "openai" {
			log.Errorf("daily domain returned by plugin openai does not match plugin name")
			return false
		}
	}

	if len(seenCosts) < len(expectedCosts)-1 || len(seenCosts) > len(expectedCosts)+1 {
		log.Errorf("daily costs returned by openai plugin are very different than expected")
		return false
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

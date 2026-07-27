package internal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OpenCostAllocationResponse maps to the OpenCost /allocation/compute structure.
type OpenCostAllocationResponse struct {
	Code int `json:"code"`
	Data []map[string]struct {
		Name      string  `json:"name"`
		TotalCost float64 `json:"totalCost"`
	} `json:"data"`
}

// FetchPodCosts queries OpenCost API for the given time window and returns a map of namespace/pod to their total cost.
func FetchPodCosts(opencostURL string, start, end time.Time) (map[string]float64, error) {
	window := fmt.Sprintf("%s,%s", start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	url := fmt.Sprintf("%s/allocation/compute?window=%s&aggregate=namespace,pod", opencostURL, window)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch opencost allocations: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opencost API returned non-200 status: %d", resp.StatusCode)
	}

	var allocResp OpenCostAllocationResponse
	if err := json.NewDecoder(resp.Body).Decode(&allocResp); err != nil {
		return nil, fmt.Errorf("failed to decode opencost response: %v", err)
	}

	podCosts := make(map[string]float64)
	for _, dataset := range allocResp.Data {
		for key, alloc := range dataset {
			podCosts[key] = alloc.TotalCost
		}
	}

	return podCosts, nil
}

// FetchNodeCosts queries OpenCost API for the given time window and returns a map of node names to their total cost.
func FetchNodeCosts(opencostURL string, start, end time.Time) (map[string]float64, error) {
	window := fmt.Sprintf("%s,%s", start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	url := fmt.Sprintf("%s/allocation/compute?window=%s&aggregate=node", opencostURL, window)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch opencost node allocations: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opencost API returned non-200 status: %d", resp.StatusCode)
	}

	var allocResp OpenCostAllocationResponse
	if err := json.NewDecoder(resp.Body).Decode(&allocResp); err != nil {
		return nil, fmt.Errorf("failed to decode opencost response: %v", err)
	}

	nodeCosts := make(map[string]float64)
	for _, dataset := range allocResp.Data {
		for key, alloc := range dataset {
			nodeCosts[key] = alloc.TotalCost
		}
	}

	return nodeCosts, nil
}

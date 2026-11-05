package ocostsplitterplugin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AllocationRow is one namespace's allocation entry from the OpenCost API.
// Only the fields the split needs are parsed; OpenCost returns more.
type AllocationRow struct {
	Namespace string  `json:"name"`
	TotalCost float64 `json:"totalCost"`
	SharedCost float64 `json:"sharedCost"`
}

type allocationResponse struct {
	Code int             `json:"code"`
	Data []AllocationRow `json:"data"`
}

// FetchAllocations queries the OpenCost allocation API for one window,
// aggregated by namespace. Window is formatted as "start,end" in RFC3339,
// per the OpenCost allocation API contract.
func FetchAllocations(apiAddress string, start, end time.Time, client *http.Client) ([]AllocationRow, error) {
	window := fmt.Sprintf("%s,%s",
		start.UTC().Format(time.RFC3339),
		end.UTC().Format(time.RFC3339))
	url := fmt.Sprintf("%s/allocation?window=%s&aggregate=namespace",
		trimSlash(apiAddress), window)

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("opencost allocation API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<24))
	if err != nil {
		return nil, fmt.Errorf("read allocation response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opencost allocation API returned HTTP %d: %s",
			resp.StatusCode, truncate(string(body), 200))
	}

	var parsed allocationResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode allocation response: %w", err)
	}
	if parsed.Code != 0 && parsed.Code != 200 {
		return nil, fmt.Errorf("opencost allocation API returned code %d", parsed.Code)
	}
	return parsed.Data, nil
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

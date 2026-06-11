package openaiplugin

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// CostsResponse represents GET /v1/organization/costs with group_by=line_item,project_id
type CostsResponse struct {
	Object   string            `json:"object"`
	Data     []CostsTimeBucket `json:"data"`
	HasMore  bool              `json:"has_more"`
	NextPage string            `json:"next_page"`
}

type CostsTimeBucket struct {
	Object    string        `json:"object"`
	StartTime int64         `json:"start_time"`
	EndTime   int64         `json:"end_time"`
	Results   []CostsResult `json:"results"`
}

type CostsResult struct {
	Object    string     `json:"object"`
	Amount    CostAmount `json:"amount"`
	LineItem  *string    `json:"line_item"`
	ProjectID *string    `json:"project_id"`
}

// CostAmount handles the API returning value as either a number or a string.
type CostAmount struct {
	Value    float64
	Currency string
}

func (c *CostAmount) UnmarshalJSON(data []byte) error {
	var raw struct {
		Value    json.RawMessage `json:"value"`
		Currency string          `json:"currency"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Currency = raw.Currency

	var f float64
	if err := json.Unmarshal(raw.Value, &f); err == nil {
		c.Value = f
		return nil
	}

	var s string
	if err := json.Unmarshal(raw.Value, &s); err == nil {
		parsed, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("cannot parse amount value %q as float: %w", s, err)
		}
		c.Value = parsed
		return nil
	}

	return fmt.Errorf("cannot unmarshal amount value: %s", string(raw.Value))
}

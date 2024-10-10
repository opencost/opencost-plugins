package openaiplugin

// OpenAIBilling represents the structure of the response JSON
type OpenAIBilling struct {
	Object string        `json:"object"`
	Data   []BillingData `json:"data"`
}

// BillingData represents the individual Billing data entries
type BillingData struct {
	Timestamp        float64 `json:"timestamp"`
	Currency         string  `json:"currency"`
	Name             string  `json:"name"`
	Cost             float64 `json:"cost"`
	OrganizationID   string  `json:"organization_id"`
	ProjectID        string  `json:"project_id"`
	ProjectName      string  `json:"project_name"`
	OrganizationName string  `json:"organization_name"`
	CostInMajorStr   string  `json:"cost_in_major"`
	CostInMajor      float64 `json:"-"`
	Date             string  `json:"date"`
}

package confluentplugin

// ConfluentCost represents a single cost entry from the Confluent Billing v1 API.
// Based on the BillingV1Cost schema from the official Confluent Cloud API.
type ConfluentCost struct {
	APIVersion        string              `json:"api_version,omitempty"`
	Kind              string              `json:"kind,omitempty"`
	ID                string              `json:"id,omitempty"`
	StartDate         string              `json:"start_date,omitempty"`
	EndDate           string              `json:"end_date,omitempty"`
	Granularity       string              `json:"granularity,omitempty"`
	LineType          string              `json:"line_type,omitempty"`
	Product           string              `json:"product,omitempty"`
	NetworkAccessType string              `json:"network_access_type,omitempty"`
	Quantity          float64             `json:"quantity,omitempty"`
	Unit              string              `json:"unit,omitempty"`
	Price             float64             `json:"price,omitempty"`
	Amount            float64             `json:"amount,omitempty"`
	OriginalAmount    float64             `json:"original_amount,omitempty"`
	DiscountAmount    float64             `json:"discount_amount,omitempty"`
	Resource          ConfluentResource   `json:"resource,omitempty"`
}

// ConfluentResource represents the resource associated with a cost entry.
type ConfluentResource struct {
	ID          string                  `json:"id,omitempty"`
	DisplayName string                  `json:"display_name,omitempty"`
	Environment ConfluentEnvironment    `json:"environment,omitempty"`
}

// ConfluentEnvironment represents the environment associated with a resource.
type ConfluentEnvironment struct {
	ID string `json:"id,omitempty"`
}

// ConfluentCostList represents the paginated list response from the Confluent Billing API.
type ConfluentCostList struct {
	APIVersion string           `json:"api_version,omitempty"`
	Kind       string           `json:"kind,omitempty"`
	Metadata   ConfluentListMeta `json:"metadata,omitempty"`
	Data       []ConfluentCost  `json:"data,omitempty"`
}

// ConfluentListMeta represents pagination metadata in list responses.
type ConfluentListMeta struct {
	TotalSize  int    `json:"total_size,omitempty"`
	PageSize   int    `json:"page_size,omitempty"`
	PageToken  string `json:"page_token,omitempty"`
	NextPageToken string `json:"next_page_token,omitempty"`
}

package cloudflareplugin

// Cloudflare API response types
// Based on Cloudflare API: https://developers.cloudflare.com/api/

// CloudflareBillingHistory represents a billing history entry
type CloudflareBillingHistory struct {
	ID          string                     `json:"id"`
	Type        string                     `json:"type"`
	Action      string                     `json:"action"`
	Description string                     `json:"description"`
	OccurredAt  string                     `json:"occurred_at"`
	CreatedAt   string                     `json:"created_at"`
	Currency    string                     `json:"currency"`
	Amount      float32                    `json:"amount"`
	TypeInfo    CloudflareBillingTypeInfo  `json:"type_info"`
}

// CloudflareBillingTypeInfo contains details about the billing type
type CloudflareBillingTypeInfo struct {
	Description string  `json:"description"`
	Unit        string  `json:"unit"`
	Quantity    float32 `json:"quantity"`
	Item       string  `json:"item"`
}

// CloudflareBillingResponse is the API response for billing history
type CloudflareBillingResponse struct {
	Result     []CloudflareBillingHistory `json:"result"`
	Success    bool                       `json:"success"`
	Errors     []CloudflareAPIError       `json:"errors"`
	Messages   []string                   `json:"messages"`
	ResultInfo CloudflareResultInfo       `json:"result_info"`
}

// CloudflareZoneBilling represents zone-level billing info
type CloudflareZoneBilling struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Plan   string  `json:"plan"`
	Status string  `json:"status"`
	Cost   float32 `json:"cost"`
}

// CloudflareZone represents a Cloudflare zone
type CloudflareZone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Plan   CloudflareZonePlan `json:"plan"`
}

// CloudflareZonePlan represents the plan on a zone
type CloudflareZonePlan struct {
	ID        string                             `json:"id"`
	Name      string                             `json:"name"`
	Price     float32                            `json:"price"`
	Frequency string                             `json:"frequency"`
	Legacy    bool                               `json:"legacy"`
	IsSub     bool                               `json:"is_subscribed"`
}

// CloudflareZonesResponse is the API response for zones
type CloudflareZonesResponse struct {
	Result     []CloudflareZone     `json:"result"`
	Success    bool                 `json:"success"`
	Errors     []CloudflareAPIError `json:"errors"`
	ResultInfo CloudflareResultInfo `json:"result_info"`
}

// CloudflareAPIError represents an API error
type CloudflareAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// CloudflareResultInfo contains pagination info
type CloudflareResultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	TotalPages int `json:"total_pages"`
	Count      int `json:"count"`
	TotalCount int `json:"total_count"`
}

// CloudflareBillingLineItem represents a line item from billing
type CloudflareBillingLineItem struct {
	Description  string  `json:"description"`
	Amount       float32 `json:"amount"`
	Currency     string  `json:"currency"`
	Quantity     float32 `json:"quantity"`
	Unit         string  `json:"unit"`
	ZoneName     string  `json:"zone_name,omitempty"`
	Product      string  `json:"product,omitempty"`
	OccurredAt   string  `json:"occurred_at"`
	Subscription bool    `json:"subscription,omitempty"`
}

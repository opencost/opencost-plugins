package githubbillingplugin

// GitHubBillingUsageItem represents a single usage item from the GitHub Billing API.
type GitHubBillingUsageItem struct {
	Date             string  `json:"date,omitempty"`
	Product          string  `json:"product,omitempty"`
	SKU              string  `json:"sku,omitempty"`
	Quantity         int64   `json:"quantity,omitempty"`
	UnitType         string  `json:"unitType,omitempty"`
	PricePerUnit     float64 `json:"pricePerUnit,omitempty"`
	GrossAmount      float64 `json:"grossAmount,omitempty"`
	DiscountAmount   float64 `json:"discountAmount,omitempty"`
	NetAmount        float64 `json:"netAmount,omitempty"`
	OrganizationName string  `json:"organizationName,omitempty"`
	RepositoryName   string  `json:"repositoryName,omitempty"`
}

// GitHubBillingUsageResponse represents the response from the GitHub Billing usage API.
type GitHubBillingUsageResponse struct {
	UsageItems []GitHubBillingUsageItem `json:"usageItems,omitempty"`
}

// GitHubBillingPremiumUsageItem represents a premium request usage item.
type GitHubBillingPremiumUsageItem struct {
	Product          string  `json:"product,omitempty"`
	SKU              string  `json:"sku,omitempty"`
	Model            string  `json:"model,omitempty"`
	UnitType         string  `json:"unitType,omitempty"`
	PricePerUnit     float64 `json:"pricePerUnit,omitempty"`
	GrossQuantity    float64 `json:"grossQuantity,omitempty"`
	GrossAmount      float64 `json:"grossAmount,omitempty"`
	DiscountQuantity float64 `json:"discountQuantity,omitempty"`
	DiscountAmount   float64 `json:"discountAmount,omitempty"`
	NetQuantity      float64 `json:"netQuantity,omitempty"`
	NetAmount        float64 `json:"netAmount,omitempty"`
}

// GitHubBillingPremiumUsageResponse represents the response from the premium request usage API.
type GitHubBillingPremiumUsageResponse struct {
	TimePeriod   GitHubBillingTimePeriod     `json:"timePeriod,omitempty"`
	Organization string                      `json:"organization,omitempty"`
	User         string                      `json:"user,omitempty"`
	Product      string                      `json:"product,omitempty"`
	Model        string                      `json:"model,omitempty"`
	UsageItems   []GitHubBillingPremiumUsageItem `json:"usageItems,omitempty"`
}

// GitHubBillingTimePeriod represents a time period in billing responses.
type GitHubBillingTimePeriod struct {
	Year  int32 `json:"year,omitempty"`
	Month int32 `json:"month,omitempty"`
	Day   int32 `json:"day,omitempty"`
}

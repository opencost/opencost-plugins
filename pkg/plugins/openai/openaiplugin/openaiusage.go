package openaiplugin

type OpenAIUsage struct {
	Object string      `json:"object"`
	Data   []UsageData `json:"data"`
}

type UsageData struct {
	OrganizationID            string  `json:"organization_id"`
	OrganizationName          string  `json:"organization_name"`
	AggregationTimestamp      int     `json:"aggregation_timestamp"`
	NRequests                 int     `json:"n_requests"`
	Operation                 string  `json:"operation"`
	SnapshotID                string  `json:"snapshot_id"`
	NContextTokensTotal       int     `json:"n_context_tokens_total"`
	NGeneratedTokensTotal     int     `json:"n_generated_tokens_total"`
	Email                     *string `json:"email"`
	APIKeyID                  *string `json:"api_key_id"`
	APIKeyName                *string `json:"api_key_name"`
	APIKeyRedacted            *string `json:"api_key_redacted"`
	APIKeyType                *string `json:"api_key_type"`
	ProjectID                 *string `json:"project_id"`
	ProjectName               *string `json:"project_name"`
	RequestType               string  `json:"request_type"`
	NCachedContextTokensTotal int     `json:"n_cached_context_tokens_total"`
}

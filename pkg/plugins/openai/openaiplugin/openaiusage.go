package openaiplugin

// CompletionsUsageResponse represents GET /v1/organization/usage/completions
// with group_by=model,project_id
type CompletionsUsageResponse struct {
	Object   string                    `json:"object"`
	Data     []CompletionsTimeBucket   `json:"data"`
	HasMore  bool                      `json:"has_more"`
	NextPage string                    `json:"next_page"`
}

type CompletionsTimeBucket struct {
	Object    string                     `json:"object"`
	StartTime int64                      `json:"start_time"`
	EndTime   int64                      `json:"end_time"`
	Results   []CompletionsUsageResult   `json:"results"`
}

type CompletionsUsageResult struct {
	Object            string  `json:"object"`
	InputTokens       int     `json:"input_tokens"`
	OutputTokens      int     `json:"output_tokens"`
	InputCachedTokens int     `json:"input_cached_tokens"`
	InputAudioTokens  int     `json:"input_audio_tokens"`
	OutputAudioTokens int     `json:"output_audio_tokens"`
	NumModelRequests  int     `json:"num_model_requests"`
	ProjectID         *string `json:"project_id"`
	UserID            *string `json:"user_id"`
	APIKeyID          *string `json:"api_key_id"`
	Model             *string `json:"model"`
	Batch             *bool   `json:"batch"`
}

package datadog

type DatadogConfig struct {
	DDSite     string `json:"datadog_site"`
	DDAPIKey   string `json:"datadog_api_key"`
	DDAppKey   string `json:"datadog_app_key"`
	DDLogLevel string `json:"log_level"`
}

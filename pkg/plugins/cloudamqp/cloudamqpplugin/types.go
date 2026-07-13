package cloudamqpplugin

// CloudAMQP API response types
// Based on CloudAMQP API: https://docs.cloudamqp.com/

type CloudAMQPInstance struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Plan     string `json:"plan"`
	Region   string `json:"region"`
	Vhost    string `json:"vhost"`
	Type     string `json:"type"`
	Created  string `json:"created"`
	URL      string `json:"url"`
	NoNodes  int    `json:"nodes"`
	Ready    bool   `json:"ready"`
	Tags     []string `json:"tags"`
}

type CloudAMQPInvoice struct {
	ID           int              `json:"id"`
	AmountCents  int              `json:"amount_cents"`
	Currency     string           `json:"currency"`
	Description  string           `json:"description"`
	PeriodStart  string           `json:"period_start"`
	PeriodEnd    string           `json:"period_end"`
	State        string           `json:"state"`
	Items        []CloudAMQPLineItem `json:"items"`
}

type CloudAMQPLineItem struct {
	Description   string  `json:"description"`
	AmountCents   int     `json:"amount_cents"`
	Quantity      float32 `json:"quantity"`
	Unit          string  `json:"unit"`
	PeriodStart   string  `json:"period_start"`
	PeriodEnd     string  `json:"period_end"`
	InstanceID    int     `json:"instance_id"`
	InstanceName  string  `json:"instance_name"`
	Plan          string  `json:"plan"`
}

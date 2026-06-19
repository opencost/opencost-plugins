package cloudamqpplugin

// CloudAMQPInstance represents a CloudAMQP instance from the Customer API.
type CloudAMQPInstance struct {
	ID           int64            `json:"id,omitempty"`
	Name         string           `json:"name,omitempty"`
	Plan         string           `json:"plan,omitempty"`
	PlanID       int64            `json:"plan_id,omitempty"`
	Region       string           `json:"region,omitempty"`
	Vhost        string           `json:"vhost,omitempty"`
	Tags         []string         `json:"tags,omitempty"`
	URL          string           `json:"url,omitempty"`
	CreatedAt    string           `json:"created_at,omitempty"`
	Nodes        int64            `json:"nodes,omitempty"`
	NodeType     string           `json:"node_type,omitempty"`
	Ready        bool             `json:"ready,omitempty"`
	Price        CloudAMQPPrice   `json:"price,omitempty"`
	NoDefaultAlarms bool          `json:"no_default_alarms,omitempty"`
	Type         string           `json:"type,omitempty"`
}

// CloudAMQPPrice represents the pricing info for a CloudAMQP instance.
type CloudAMQPPrice struct {
	Amount   float64 `json:"amount,omitempty"`
	Currency string  `json:"currency,omitempty"`
	Interval string  `json:"interval,omitempty"`
}

// CloudAMQPInvoice represents an invoice from the CloudAMQP Customer API.
type CloudAMQPInvoice struct {
	ID           int64                `json:"id,omitempty"`
	Amount       float64              `json:"amount,omitempty"`
	Currency     string               `json:"currency,omitempty"`
	Description  string               `json:"description,omitempty"`
	PeriodStart  string               `json:"period_start,omitempty"`
	PeriodEnd    string               `json:"period_end,omitempty"`
	Status       string               `json:"status,omitempty"`
	Items        []CloudAMQPInvoiceItem `json:"items,omitempty"`
}

// CloudAMQPInvoiceItem represents a line item in a CloudAMQP invoice.
type CloudAMQPInvoiceItem struct {
	Description string  `json:"description,omitempty"`
	Amount      float64 `json:"amount,omitempty"`
	Quantity    float64 `json:"quantity,omitempty"`
	PeriodStart string  `json:"period_start,omitempty"`
	PeriodEnd   string  `json:"period_end,omitempty"`
	InstanceID  int64   `json:"instance_id,omitempty"`
	InstanceName string `json:"instance_name,omitempty"`
	Plan         string `json:"plan,omitempty"`
}

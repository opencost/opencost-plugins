package fastlyplugin

// FastlyInvoice represents a Fastly invoice from the billing API.
type FastlyInvoice struct {
	InvoiceID               string                  `json:"invoice_id"`
	CustomerID              string                  `json:"customer_id"`
	CurrencyCode            string                  `json:"currency_code"`
	BillingStartDate        string                  `json:"billing_start_date"`
	BillingEndDate          string                  `json:"billing_end_date"`
	StatementNumber         string                  `json:"statement_number"`
	InvoicePostedOn         string                  `json:"invoice_posted_on"`
	MonthlyTransactionAmount float64                `json:"monthly_transaction_amount"`
	TransactionLineItems    []FastlyTransactionLine `json:"transaction_line_items"`
}

// FastlyTransactionLine represents a single line item in a Fastly invoice.
type FastlyTransactionLine struct {
	Description      string  `json:"description"`
	ProductGroup     string  `json:"product_group"`
	ProductLine      string  `json:"product_line"`
	ProductName      string  `json:"product_name"`
	UsageType        string  `json:"usage_type"`
	Region           string  `json:"region"`
	Units            float64 `json:"units"`
	Rate             float64 `json:"rate"`
	Amount           float64 `json:"amount"`
	CreditCouponCode string  `json:"credit_coupon_code"`
}

// FastlyInvoiceListResponse represents the paginated response from listing invoices.
type FastlyInvoiceListResponse struct {
	Meta     FastlyInvoiceMeta `json:"meta"`
	Invoices []FastlyInvoice   `json:"data"`
}

// FastlyInvoiceMeta represents pagination metadata.
type FastlyInvoiceMeta struct {
	Limit      int    `json:"limit"`
	NextCursor string `json:"next_cursor"`
	Sort       string `json:"sort"`
	Total      int    `json:"total"`
}

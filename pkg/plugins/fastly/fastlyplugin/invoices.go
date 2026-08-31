package fastlyplugin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type InvoiceListResponse struct {
	Data []Invoice          `json:"data"`
	Meta PaginationMetadata `json:"meta"`
}

type Invoice struct {
	CustomerID               string                `json:"customer_id"`
	InvoiceID                string                `json:"invoice_id"`
	InvoicePostedOn          string                `json:"invoice_posted_on"`
	BillingStartDate         string                `json:"billing_start_date"`
	BillingEndDate           string                `json:"billing_end_date"`
	StatementNumber          string                `json:"statement_number"`
	CurrencyCode             string                `json:"currency_code"`
	MonthlyTransactionAmount string                `json:"monthly_transaction_amount"`
	PaymentStatus            string                `json:"payment_status,omitempty"`
	TransactionLineItems     []TransactionLineItem `json:"transaction_line_items"`
}

type TransactionLineItem struct {
	Description      string  `json:"description"`
	Amount           float64 `json:"amount"`
	CreditCouponCode string  `json:"credit_coupon_code"`
	Rate             float64 `json:"rate"`
	Units            float64 `json:"units"`
	ProductName      string  `json:"product_name"`
	ProductGroup     string  `json:"product_group"`
	ProductLine      string  `json:"product_line"`
	Region           string  `json:"region"`
	UsageType        string  `json:"usage_type"`
}

type PaginationMetadata struct {
	NextCursor string `json:"next_cursor"`
	Limit      int    `json:"limit"`
	Total      int    `json:"total"`
	Sort       string `json:"sort"`
}

// Custom UnmarshalJSON for Invoice to handle monthly_transaction_amount 
// which can come as either a number or string from the API
func (i *Invoice) UnmarshalJSON(data []byte) error {
	// Define a temporary struct with the same fields but using interface{} for monthly_transaction_amount
	type TempInvoice struct {
		CustomerID               string                `json:"customer_id"`
		InvoiceID                string                `json:"invoice_id"`
		InvoicePostedOn          string                `json:"invoice_posted_on"`
		BillingStartDate         string                `json:"billing_start_date"`
		BillingEndDate           string                `json:"billing_end_date"`
		StatementNumber          string                `json:"statement_number"`
		CurrencyCode             string                `json:"currency_code"`
		MonthlyTransactionAmount interface{}           `json:"monthly_transaction_amount"`
		PaymentStatus            string                `json:"payment_status,omitempty"`
		TransactionLineItems     []TransactionLineItem `json:"transaction_line_items"`
	}
	
	var temp TempInvoice
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}
	
	// Copy all fields except MonthlyTransactionAmount
	i.CustomerID = temp.CustomerID
	i.InvoiceID = temp.InvoiceID
	i.InvoicePostedOn = temp.InvoicePostedOn
	i.BillingStartDate = temp.BillingStartDate
	i.BillingEndDate = temp.BillingEndDate
	i.StatementNumber = temp.StatementNumber
	i.CurrencyCode = temp.CurrencyCode
	i.PaymentStatus = temp.PaymentStatus
	i.TransactionLineItems = temp.TransactionLineItems
	
	// Handle MonthlyTransactionAmount conversion
	switch v := temp.MonthlyTransactionAmount.(type) {
	case string:
		i.MonthlyTransactionAmount = v
	case float64:
		i.MonthlyTransactionAmount = strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		i.MonthlyTransactionAmount = strconv.Itoa(v)
	default:
		if v == nil {
			i.MonthlyTransactionAmount = ""
		} else {
			return fmt.Errorf("unsupported type for monthly_transaction_amount: %T", v)
		}
	}
	
	return nil
}

// Helper function to parse date strings
func ParseFastlyDate(dateStr string) (time.Time, error) {
	return time.Parse(time.RFC3339, dateStr)
}

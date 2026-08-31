package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-plugin"
	commonconfig "github.com/opencost/opencost-plugins/common/config"
	fastlyconfig "github.com/opencost/opencost-plugins/pkg/plugins/fastly/config"
	"github.com/opencost/opencost-plugins/pkg/plugins/fastly/fastlyplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	fastlyAPIBaseURL = "https://api.fastly.com"
)

// handshakeConfigs are used to just do a basic handshake between
// a plugin and host. If the handshake fails, a user friendly error is shown.
// This prevents users from executing bad plugins or executing a plugin
// directory. It is a UX feature, not a security feature.
var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "fastly",
}

// HTTPClient interface for better testability
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Implementation of CustomCostSource
type FastlyCostSource struct {
	apiKey          string
	httpClient      HTTPClient // Changed from *http.Client to HTTPClient interface
	rateLimiter     *rate.Limiter
	invoiceCache    map[string][]fastlyplugin.Invoice
	invoiceCacheMux sync.Mutex
}

// validateRequest validates the incoming request and returns any errors
func validateRequest(req *pb.CustomCostRequest) []string {
	var errors []string
	now := time.Now()

	// 1. Check if resolution is less than an hour (Fastly supports hourly data)
	if req.Resolution.AsDuration() < time.Hour {
		resolutionMessage := "Resolution should be at least one hour for Fastly billing data"
		log.Warn(resolutionMessage)
		errors = append(errors, resolutionMessage)
	}

	// 2. Check if the date range is reasonable (not too far in the past)
	// Fastly typically keeps detailed billing data for the last 12 months
	twelveMonthsAgo := now.AddDate(0, -12, 0)
	if req.Start.AsTime().Before(twelveMonthsAgo) {
		startDateMessage := fmt.Sprintf("Start date is more than 12 months in the past. Fastly billing data may not be available before %s",
			twelveMonthsAgo.Format("2006-01-02"))
		log.Warn(startDateMessage)
		errors = append(errors, startDateMessage)
	}

	// 3. Check if end time is after start time
	if req.End.AsTime().Before(req.Start.AsTime()) {
		dateRangeMessage := "End date cannot be before start date"
		log.Error(dateRangeMessage)
		errors = append(errors, dateRangeMessage)
	}

	// Note: Future date validation is handled earlier in GetCustomCosts to return empty response

	// 5. Warn if the date range is very large (performance consideration)
	daysDiff := req.End.AsTime().Sub(req.Start.AsTime()).Hours() / 24
	if daysDiff > 90 {
		performanceMessage := fmt.Sprintf("Large date range requested (%.0f days). This may take longer to process", daysDiff)
		log.Info(performanceMessage)
		// This is just a warning, not an error
	}

	return errors
}

func (f *FastlyCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
	results := []*pb.CustomCostResponse{}

	// Check if requesting future data - return empty response if so
	now := time.Now().UTC()
	if req.Start.AsTime().After(now) || req.End.AsTime().After(now.Add(time.Hour)) {
		log.Debugf("skipping future window request: start=%v, end=%v", req.Start.AsTime(), req.End.AsTime())
		return results // Return empty array for future windows
	}

	// Validate the request for other issues
	requestErrors := validateRequest(req)
	if len(requestErrors) > 0 {
		// Return error response if validation fails
		startTime := req.Start.AsTime()
		endTime := req.End.AsTime()
		errResp := boilerplateFastlyCustomCost(opencost.NewWindow(&startTime, &endTime))
		errResp.Errors = requestErrors
		results = append(results, &errResp)
		return results
	}

	targets, err := opencost.GetWindows(req.Start.AsTime(), req.End.AsTime(), req.Resolution.AsDuration())
	if err != nil {
		log.Errorf("error getting windows: %v", err)
		startTime := req.Start.AsTime()
		endTime := req.End.AsTime()
		errResp := boilerplateFastlyCustomCost(opencost.NewWindow(&startTime, &endTime))
		errResp.Errors = []string{fmt.Sprintf("error getting windows: %v", err)}
		results = append(results, &errResp)
		return results
	}

	// Fetch all invoices at once for the entire period
	startTime := req.Start.AsTime()
	endTime := req.End.AsTime()
	allInvoices, err := f.getInvoicesForPeriod(&startTime, &endTime)

	if err != nil {
		log.Errorf("error fetching invoices for period: %v", err)
		errResp := boilerplateFastlyCustomCost(opencost.NewWindow(&startTime, &endTime))
		errResp.Errors = []string{fmt.Sprintf("error fetching invoices: %v", err)}
		results = append(results, &errResp)
		return results
	}

	// Store in cache with consistent format (day-level granularity)
	cacheKey := fmt.Sprintf("%s_%s", req.Start.AsTime().Format("2006-01-02"), req.End.AsTime().Format("2006-01-02"))
	f.invoiceCacheMux.Lock()
	f.invoiceCache[cacheKey] = allInvoices
	f.invoiceCacheMux.Unlock()

	for _, target := range targets {
		// Skip future windows
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		log.Debugf("fetching Fastly costs for window %v", target)
		result := f.getFastlyCostsForWindow(target, allInvoices)
		results = append(results, result)
	}

	return results
}

func (f *FastlyCostSource) getFastlyCostsForWindow(window opencost.Window, allInvoices []fastlyplugin.Invoice) *pb.CustomCostResponse {
	ccResp := boilerplateFastlyCustomCost(window)
	costs := []*pb.CustomCost{}

	// Filter invoices that overlap with this window
	for _, invoice := range allInvoices {
		invoiceCosts := f.convertInvoiceToCosts(invoice, window)
		costs = append(costs, invoiceCosts...)
	}

	ccResp.Costs = costs
	return &ccResp
}

func (f *FastlyCostSource) getInvoicesForPeriod(start, end *time.Time) ([]fastlyplugin.Invoice, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("%s_%s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	f.invoiceCacheMux.Lock()
	if cached, ok := f.invoiceCache[cacheKey]; ok {
		f.invoiceCacheMux.Unlock()
		log.Debugf("returning cached invoices for period %s to %s", start.Format("2006-01-02"), end.Format("2006-01-02"))
		return cached, nil
	}
	f.invoiceCacheMux.Unlock()

	allInvoices := []fastlyplugin.Invoice{}
	cursor := ""
	hasMore := true

	// Format dates for API - ensure we use proper date format
	startStr := start.Format("2006-01-02")
	endStr := end.Format("2006-01-02")

	for hasMore {
		// Rate limiting
		if f.rateLimiter.Tokens() < 1.0 {
			log.Infof("fastly rate limit reached. holding request until rate capacity is back")
		}
		err := f.rateLimiter.WaitN(context.TODO(), 1)
		if err != nil {
			return nil, fmt.Errorf("error waiting on rate limiter: %v", err)
		}

		// Build request URL
		reqURL := fmt.Sprintf("%s/billing/v3/invoices", fastlyAPIBaseURL)
		params := url.Values{}
		params.Add("billing_start_date", startStr)
		params.Add("billing_end_date", endStr)
		params.Add("limit", "200")
		if cursor != "" {
			params.Add("cursor", cursor)
		}
		reqURL = fmt.Sprintf("%s?%s", reqURL, params.Encode())

		log.Debugf("Fetching invoices from: %s", reqURL)

		// Make request
		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("error creating request: %v", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Fastly-Key", f.apiKey)

		resp, err := f.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error making request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
		}

		// Parse response
		var invoiceResp fastlyplugin.InvoiceListResponse
		if err := json.NewDecoder(resp.Body).Decode(&invoiceResp); err != nil {
			return nil, fmt.Errorf("error decoding response: %v", err)
		}

		log.Debugf("Retrieved %d invoices, total: %d", len(invoiceResp.Data), invoiceResp.Meta.Total)
		allInvoices = append(allInvoices, invoiceResp.Data...)

		// Check for more pages
		if invoiceResp.Meta.NextCursor == "" {
			hasMore = false
		} else {
			cursor = invoiceResp.Meta.NextCursor
		}
	}

	// Also get month-to-date if the window includes current month
	now := time.Now()
	if start.Before(now) && end.After(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)) {
		mtdInvoice, err := f.getMonthToDateInvoice()
		if err != nil {
			log.Warnf("error fetching month-to-date invoice: %v", err)
		} else if mtdInvoice != nil {
			// Check for duplicates before adding MTD invoice
			// MTD invoice should not be in the regular invoice list, but we check to be safe
			isDuplicate := false
			for _, existingInvoice := range allInvoices {
				if existingInvoice.InvoiceID == mtdInvoice.InvoiceID {
					log.Debugf("Month-to-date invoice %s already exists in invoice list, skipping", mtdInvoice.InvoiceID)
					isDuplicate = true
					break
				}
			}

			if !isDuplicate {
				log.Debugf("Including month-to-date invoice: %s", mtdInvoice.InvoiceID)
				allInvoices = append(allInvoices, *mtdInvoice)
			}
		}
	}

	// Store in cache
	f.invoiceCacheMux.Lock()
	f.invoiceCache[cacheKey] = allInvoices
	f.invoiceCacheMux.Unlock()

	log.Infof("Total invoices retrieved for period %s to %s: %d", startStr, endStr, len(allInvoices))
	return allInvoices, nil
}

func (f *FastlyCostSource) getMonthToDateInvoice() (*fastlyplugin.Invoice, error) {
	// Rate limiting
	if f.rateLimiter.Tokens() < 1.0 {
		log.Infof("fastly rate limit reached. holding request until rate capacity is back")
	}
	err := f.rateLimiter.WaitN(context.TODO(), 1)
	if err != nil {
		return nil, fmt.Errorf("error waiting on rate limiter: %v", err)
	}

	reqURL := fmt.Sprintf("%s/billing/v3/invoices/month-to-date", fastlyAPIBaseURL)
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Fastly-Key", f.apiKey)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var invoice fastlyplugin.Invoice
	if err := json.NewDecoder(resp.Body).Decode(&invoice); err != nil {
		return nil, fmt.Errorf("error decoding response: %v", err)
	}

	return &invoice, nil
}

func (f *FastlyCostSource) getInvoiceByID(invoiceID string) (*fastlyplugin.Invoice, error) {
	// Rate limiting
	if f.rateLimiter.Tokens() < 1.0 {
		log.Infof("fastly rate limit reached. holding request until rate capacity is back")
	}
	err := f.rateLimiter.WaitN(context.TODO(), 1)
	if err != nil {
		return nil, fmt.Errorf("error waiting on rate limiter: %v", err)
	}

	reqURL := fmt.Sprintf("%s/billing/v3/invoices/%s", fastlyAPIBaseURL, invoiceID)
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Fastly-Key", f.apiKey)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var invoice fastlyplugin.Invoice
	if err := json.NewDecoder(resp.Body).Decode(&invoice); err != nil {
		return nil, fmt.Errorf("error decoding response: %v", err)
	}

	return &invoice, nil
}

func (f *FastlyCostSource) convertInvoiceToCosts(invoice fastlyplugin.Invoice, window opencost.Window) []*pb.CustomCost {
	costs := []*pb.CustomCost{}

	// Parse invoice dates
	startDate, err := fastlyplugin.ParseFastlyDate(invoice.BillingStartDate)
	if err != nil {
		log.Errorf("error parsing billing start date for invoice %s: %v", invoice.InvoiceID, err)
		return costs
	}
	endDate, err := fastlyplugin.ParseFastlyDate(invoice.BillingEndDate)
	if err != nil {
		log.Errorf("error parsing billing end date for invoice %s: %v", invoice.InvoiceID, err)
		return costs
	}

	// Check if invoice overlaps with window
	invoiceStart := startDate
	invoiceEnd := endDate
	windowStart := *window.Start()
	windowEnd := *window.End()

	if invoiceEnd.Before(windowStart) || invoiceStart.After(windowEnd) {
		return costs
	}

	// Calculate the overlap duration for prorating
	overlapStart := invoiceStart
	if windowStart.After(invoiceStart) {
		overlapStart = windowStart
	}
	overlapEnd := invoiceEnd
	if windowEnd.Before(invoiceEnd) {
		overlapEnd = windowEnd
	}

	overlapHours := overlapEnd.Sub(overlapStart).Hours()
	totalInvoiceHours := invoiceEnd.Sub(invoiceStart).Hours()
	prorateRatio := float32(1.0)
	if totalInvoiceHours > 0 {
		prorateRatio = float32(overlapHours / totalInvoiceHours)
	}

	log.Debugf("Invoice %s: overlap %.2f hours of %.2f total hours (%.2f%%)",
		invoice.InvoiceID, overlapHours, totalInvoiceHours, prorateRatio*100)

	// Convert each line item to a cost
	for _, item := range invoice.TransactionLineItems {
		// Skip zero-amount items unless they have units (could be usage tracking)
		if item.Amount == 0 && item.Units == 0 {
			continue
		}

		// Create provider ID using CustomerID for stability across billing periods
		// Using CustomerID instead of InvoiceID ensures the same resource is tracked consistently month-over-month
		providerID := fmt.Sprintf("%s/%s/%s", invoice.CustomerID, item.ProductName, item.UsageType)

		// Prorate the cost based on window overlap
		billedCost := float32(item.Amount) * prorateRatio
		usageQuantity := float32(item.Units) * prorateRatio

		// Handle region - default to "Global" if empty
		region := item.Region
		if region == "" {
			region = "Global"
		}

		// Create extended attributes following FOCUS mapping
		// According to fastly_focus_mapping.md:
		// AccountId maps to customer_id (Fastly customer/account ID)
		extendedAttrs := pb.CustomCostExtendedAttributes{
			AccountId: &invoice.CustomerID, // FOCUS: AccountId → customer_id
		}

		// Use product_line as SubAccountId if available for better granularity
		if item.ProductLine != "" {
			extendedAttrs.SubAccountId = &item.ProductLine // FOCUS: Subcategory → product_line
		}

		// Add billing period information
		// Parse ISO 8601 dates and convert to timestamppb.Timestamp
		if billingStartTime, err := fastlyplugin.ParseFastlyDate(invoice.BillingStartDate); err == nil {
			extendedAttrs.BillingPeriodStart = timestamppb.New(billingStartTime) // FOCUS: BillingPeriodStart → billing_start_date
		}
		if billingEndTime, err := fastlyplugin.ParseFastlyDate(invoice.BillingEndDate); err == nil {
			extendedAttrs.BillingPeriodEnd = timestamppb.New(billingEndTime) // FOCUS: BillingPeriodEnd → billing_end_date
		}

		// Add service information if available
		if item.ProductGroup != "" {
			extendedAttrs.ServiceCategory = &item.ProductGroup // FOCUS: ServiceCategory → product_group
		}
		if item.ProductName != "" {
			extendedAttrs.ServiceName = &item.ProductName // FOCUS: ServiceName → product_name
		}

		// Add pricing information
		if item.Units > 0 {
			units := float32(item.Units)
			extendedAttrs.PricingQuantity = &units // FOCUS: PricingQuantity → units
		}
		if item.UsageType != "" {
			extendedAttrs.PricingUnit = &item.UsageType // FOCUS: PricingUnit → usage_type
		}

		// Add discount code if available
		if item.CreditCouponCode != "" {
			extendedAttrs.CommitmentDiscountId = &item.CreditCouponCode // FOCUS: CommitmentDiscountId → credit_coupon_code
		}

		cost := &pb.CustomCost{
			Zone:           region,
			AccountName:    invoice.CustomerID,
			ChargeCategory: getChargeCategory(item),
			Description:    item.Description,
			ResourceName:   item.UsageType,    // UsageType goes to ResourceName
			ResourceType:   item.ProductGroup, // ProductGroup goes to ResourceType
			Id:             uuid.New().String(),
			ProviderId:     providerID,
			Labels: map[string]string{
				"product_line":       item.ProductLine,
				"product_name":       item.ProductName,
				"credit_coupon_code": item.CreditCouponCode,
				"currency":           invoice.CurrencyCode,
			},
			ListCost:           billedCost,
			ListUnitPrice:      float32(item.Rate),
			BilledCost:         billedCost,
			UsageQuantity:      usageQuantity,
			UsageUnit:          getUsageUnit(item.UsageType, item.ProductName),
			ExtendedAttributes: &extendedAttrs,
		}

		// Add additional label for credits/discounts
		if item.Amount < 0 {
			cost.Labels["cost_type"] = "credit"
		} else {
			cost.Labels["cost_type"] = "charge"
		}

		costs = append(costs, cost)
	}

	return costs
}

// Enhanced getUsageUnit function with more comprehensive mapping
func getUsageUnit(usageType string, productName string) string {
	// Normalize to lowercase for comparison
	usageTypeLower := strings.ToLower(usageType)
	productNameLower := strings.ToLower(productName)

	// Check usage type first
	switch {
	case strings.Contains(usageTypeLower, "bandwidth"):
		return "GB"
	case strings.Contains(usageTypeLower, "request"):
		return "requests"
	case strings.Contains(usageTypeLower, "compute"):
		return "compute-hours"
	case strings.Contains(usageTypeLower, "storage"):
		return "GB"
	case strings.Contains(usageTypeLower, "committed amount"):
		return "USD"
	case strings.Contains(usageTypeLower, "minute"):
		return "minutes"
	case strings.Contains(usageTypeLower, "hour"):
		return "hours"
	case strings.Contains(usageTypeLower, "invocation"):
		return "invocations"
	case strings.Contains(usageTypeLower, "log"):
		return "log-lines"
	}

	// Check product name as fallback
	switch {
	case strings.Contains(productNameLower, "cdn"):
		return "GB"
	case strings.Contains(productNameLower, "compute"):
		return "compute-hours"
	case strings.Contains(productNameLower, "waf") || strings.Contains(productNameLower, "security"):
		return "requests"
	case strings.Contains(productNameLower, "image"):
		return "transformations"
	case strings.Contains(productNameLower, "video"):
		return "minutes"
	}

	// Default
	return "units"
}

// getChargeCategory determines the charge category based on the line item
func getChargeCategory(item fastlyplugin.TransactionLineItem) string {
	descLower := strings.ToLower(item.Description)

	switch {
	case strings.Contains(descLower, "minimum"):
		return "commitment"
	case strings.Contains(descLower, "credit") || item.Amount < 0:
		return "credit"
	case strings.Contains(descLower, "support"):
		return "support"
	case strings.Contains(descLower, "tax"):
		return "tax"
	default:
		return "usage"
	}
}

func boilerplateFastlyCustomCost(win opencost.Window) pb.CustomCostResponse {
	return pb.CustomCostResponse{
		Metadata: map[string]string{
			"api_client_version": "v3",
			"plugin_version":     "v1.1.0", // Bumped version for Phase 1 enhancements
		},
		CostSource: "billing",
		Domain:     "fastly",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      []*pb.CustomCost{},
	}
}

// getFastlyHTTPClient returns an HTTP client for Fastly API with configurable timeout
func getFastlyHTTPClient(timeoutSeconds int) HTTPClient {
	return &http.Client{
		Timeout: time.Duration(timeoutSeconds) * time.Second,
	}
}

func main() {
	log.Debug("Initializing Fastly plugin")

	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	fastlyConfig, err := fastlyconfig.GetFastlyConfig(configFile)
	if err != nil {
		log.Fatalf("error building Fastly config: %v", err)
	}
	log.SetLogLevel(fastlyConfig.LogLevel)

	// Fastly rate limiting - 6000 requests per minute = 100 requests per second
	// Being slightly conservative at 90 requests per second to avoid hitting limits
	rateLimiter := rate.NewLimiter(rate.Limit(90), 100)

	fastlyCostSrc := FastlyCostSource{
		apiKey:       fastlyConfig.FastlyAPIKey,
		httpClient:   getFastlyHTTPClient(fastlyConfig.HTTPTimeoutSec),
		rateLimiter:  rateLimiter,
		invoiceCache: make(map[string][]fastlyplugin.Invoice),
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &fastlyCostSrc},
	}

	log.Infof("Starting Fastly plugin server v1.1.0...")
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

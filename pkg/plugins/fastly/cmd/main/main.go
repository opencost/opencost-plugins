package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-plugin"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	fastlyconfig "github.com/opencost/opencost-plugins/pkg/plugins/fastly/config"
	fastlyplugin "github.com/opencost/opencost-plugins/pkg/plugins/fastly/fastlyplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
)

// handshakeConfigs are used to just do a basic handshake between
// a plugin and host. If the handshake fails, a user friendly error is shown.
var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "fastly",
}

const fastlyInvoicesURL = "https://api.fastly.com/billing/v3/invoices"
const fastlyInvoiceByIDURL = "https://api.fastly.com/billing/v3/invoices/%s"
const fastlyInvoiceMTDURL = "https://api.fastly.com/billing/v3/invoices/month-to-date"
const fastlyAPIDateFormat = "2006-01-02"

// Implementation of CustomCostSource
type FastlyCostSource struct {
	rateLimiter *rate.Limiter
	config      *fastlyconfig.FastlyConfig
	client      HTTPClient
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

func (f *FastlyCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
	results := []*pb.CustomCostResponse{}

	targets, err := opencost.GetWindows(req.Start.AsTime(), req.End.AsTime(), req.Resolution.AsDuration())
	if err != nil {
		log.Errorf("error getting windows: %v", err)
		errResp := pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting windows: %v", err)},
		}
		results = append(results, &errResp)
		return results
	}

	for _, target := range targets {
		// don't allow future requests
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		log.Debugf("fetching Fastly costs for window %v", target)
		result := f.getFastlyCostsForWindow(target)
		results = append(results, result)
	}

	return results
}

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	fastlyCfg, err := fastlyconfig.GetFastlyConfig(configFile)
	if err != nil {
		log.Fatalf("error building Fastly config: %v", err)
	}
	log.SetLogLevel(fastlyCfg.LogLevel)

	// Fastly API rate limit: approximately 100 requests per minute
	rateLimiter := rate.NewLimiter(1.5, 2)
	fastlyCostSrc := FastlyCostSource{
		rateLimiter: rateLimiter,
		config:      fastlyCfg,
		client:      &http.Client{},
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &fastlyCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

func boilerplateFastlyCustomCost(win opencost.Window) pb.CustomCostResponse {
	return pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v3"},
		CostSource: "CDN",
		Domain:     "fastly",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      []*pb.CustomCost{},
	}
}

func (f *FastlyCostSource) getFastlyCostsForWindow(window opencost.Window) *pb.CustomCostResponse {
	ccResp := boilerplateFastlyCustomCost(window)

	winStart := window.Start().Format(fastlyAPIDateFormat)
	winEnd := window.End().Format(fastlyAPIDateFormat)

	// Fetch invoices for the window period
	invoices, err := f.getInvoices(winStart, winEnd)
	if err != nil {
		ccResp.Errors = append(ccResp.Errors, fmt.Sprintf("error getting Fastly invoices: %v", err))
		return &ccResp
	}

	customCosts := []*pb.CustomCost{}
	for _, invoice := range invoices {
		for _, lineItem := range invoice.TransactionLineItems {
			cost := pb.CustomCost{
				AccountName:    invoice.CustomerID,
				ChargeCategory: "Usage",
				Description:    lineItem.Description,
				ResourceName:   lineItem.ProductName,
				ResourceType:   lineItem.ProductGroup,
				Id:             uuid.New().String(),
				ProviderId:     fmt.Sprintf("%s/%s/%s", invoice.CustomerID, lineItem.ProductName, lineItem.UsageType),
				BilledCost:     float32(lineItem.Amount),
				ListCost:       float32(lineItem.Units * lineItem.Rate),
				ListUnitPrice:  float32(lineItem.Rate),
				UsageQuantity:  float32(lineItem.Units),
				UsageUnit:      lineItem.UsageType,
				Zone:           lineItem.Region,
				Labels:         map[string]string{},
			}

			if lineItem.ProductLine != "" {
				cost.Labels["product_line"] = lineItem.ProductLine
			}
			if invoice.CurrencyCode != "" {
				cost.Labels["currency_code"] = invoice.CurrencyCode
			}
			if invoice.InvoiceID != "" {
				cost.Labels["invoice_id"] = invoice.InvoiceID
			}

			customCosts = append(customCosts, &cost)
		}
	}

	ccResp.Costs = customCosts
	return &ccResp
}

func (f *FastlyCostSource) getInvoices(startDate, endDate string) ([]fastlyplugin.FastlyInvoice, error) {
	var allInvoices []fastlyplugin.FastlyInvoice

	url := fmt.Sprintf("%s?billing_start_date=%s&billing_end_date=%s&limit=200", fastlyInvoicesURL, startDate, endDate)

	for {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("error creating request: %v", err)
		}
		req.Header.Set("Fastly-Key", f.config.APIKey)
		req.Header.Set("Accept", "application/json")

		err = f.rateLimiter.Wait(nil)
		if err != nil {
			return nil, fmt.Errorf("rate limiter error: %v", err)
		}

		resp, err := f.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error making request to Fastly API: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("Fastly API returned status %d: %s", resp.StatusCode, string(body))
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading response body: %v", err)
		}

		var invoiceResp fastlyplugin.FastlyInvoiceListResponse
		err = json.Unmarshal(body, &invoiceResp)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling Fastly response: %v", err)
		}

		// Fetch detailed invoice for each invoice ID to get line items
		for _, invoice := range invoiceResp.Invoices {
			invoiceID := invoice.InvoiceID
			if invoiceID == "" {
				// If no invoice ID, use the summary data
				allInvoices = append(allInvoices, invoice)
				continue
			}

			detailedInvoice, err := f.getInvoiceByID(invoiceID)
			if err != nil {
				log.Warnf("error getting invoice details for %s: %v, using summary data", invoiceID, err)
				allInvoices = append(allInvoices, invoice)
				continue
			}

			allInvoices = append(allInvoices, *detailedInvoice)
		}

		// Check for pagination
		if invoiceResp.Meta.NextCursor != "" {
			url = fmt.Sprintf("%s?cursor=%s&limit=200", fastlyInvoicesURL, invoiceResp.Meta.NextCursor)
		} else {
			break
		}
	}

	return allInvoices, nil
}

func (f *FastlyCostSource) getInvoiceByID(invoiceID string) (*fastlyplugin.FastlyInvoice, error) {
	invoiceIDInt, err := strconv.Atoi(invoiceID)
	if err != nil {
		return nil, fmt.Errorf("invalid invoice ID %s: %v", invoiceID, err)
	}

	url := fmt.Sprintf(fastlyInvoiceByIDURL, invoiceIDInt)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Set("Fastly-Key", f.config.APIKey)
	req.Header.Set("Accept", "application/json")

	err = f.rateLimiter.Wait(nil)
	if err != nil {
		return nil, fmt.Errorf("rate limiter error: %v", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request to Fastly API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Fastly API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var invoice fastlyplugin.FastlyInvoice
	err = json.Unmarshal(body, &invoice)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling Fastly invoice response: %v", err)
	}

	return &invoice, nil
}

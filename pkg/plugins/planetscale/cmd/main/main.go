package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/icholy/digest"
	commonconfig "github.com/opencost/opencost-plugins/common/config"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/util/uuid"
)

// handshakeConfigs are used to just do a basic handshake between
// a plugin and host. If the handshake fails, a user friendly error is shown.
// This prevents users from executing bad plugins or executing a plugin
// directory. It is a UX feature, not a security feature.
var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "planetscale",
}

const costExplorerPendingInvoicesURL = "https://api.planetscale.com/v1/orgs/%s/pending-invoices"

func main() {
	log.Debug("Initializing PlanetScale plugin")

	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	planetScaleConfig, err := GetPlanetScaleConfig(configFile)
	if err != nil {
		log.Fatalf("error building PlanetScale config: %v", err)
	}
	log.SetLogLevel(planetScaleConfig.LogLevel)

	// PlanetScale API rate limits
	rateLimiter := rate.NewLimiter(1.1, 2)
	planetScaleCostSrc := PlanetScaleCostSource{
		rateLimiter: rateLimiter,
		orgID:       planetScaleConfig.OrgID,
	}
	planetScaleCostSrc.httpClient = getPlanetScaleClient(*planetScaleConfig)

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &planetScaleCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

func getPlanetScaleClient(planetScaleConfig PlanetScaleConfig) HTTPClient {
	return &http.Client{
		Transport: &digest.Transport{
			Username: planetScaleConfig.PublicKey,
			Password: planetScaleConfig.PrivateKey,
		},
	}
}

// Implementation of CustomCostSource
type PlanetScaleCostSource struct {
	orgID       string
	rateLimiter *rate.Limiter
	httpClient  HTTPClient
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

func validateRequest(req *pb.CustomCostRequest) []string {
	var errors []string
	now := time.Now()
	// 1. Check if resolution is less than a day
	if req.Resolution.AsDuration() < 24*time.Hour {
		var resolutionMessage = "Resolution should be at least one day."
		log.Warnf(resolutionMessage)
		errors = append(errors, resolutionMessage)
	}
	// Get the start of the current month
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	// 2. Check if start time is before the start of the current month
	if req.Start.AsTime().Before(currentMonthStart) {
		var startDateMessage = "Start date cannot be before the current month. Historical costs not currently supported."
		log.Warnf(startDateMessage)
		errors = append(errors, startDateMessage)
	}

	// 3. Check if end time is before the start of the current month
	if req.End.AsTime().Before(currentMonthStart) {
		var endDateMessage = "End date cannot be before the current month. Historical costs not currently supported."
		log.Warnf(endDateMessage)
		errors = append(errors, endDateMessage)
	}

	return errors
}

func (p *PlanetScaleCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
	results := []*pb.CustomCostResponse{}

	requestErrors := validateRequest(req)
	if len(requestErrors) > 0 {
		// Return empty response
		return results
	}

	targets, err := opencost.GetWindows(req.Start.AsTime(), req.End.AsTime(), req.Resolution.AsDuration())
	if err != nil {
		log.Errorf("error getting windows: %v", err)
		errResp := pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting windows: %v", err)},
		}
		results = append(results, &errResp)
		return results
	}

	lineItems, err := GetPendingInvoices(p.orgID, p.httpClient)
	if err != nil {
		log.Errorf("Error fetching invoices: %v", err)
		errResp := pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error fetching invoices: %v", err)},
		}
		results = append(results, &errResp)
		return results
	}

	for _, target := range targets {
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		log.Debugf("fetching PlanetScale costs for window %v", target)
		result := p.getPlanetScaleCostsForWindow(&target, lineItems)

		results = append(results, result)
	}

	return results
}

func filterLineItemsByWindow(win *opencost.Window, lineItems []LineItem) []*pb.CustomCost {
	var filteredItems []*pb.CustomCost

	winStartUTC := win.Start().UTC()
	winEndUTC := win.End().UTC()
	log.Debugf("Item window %s %s", winStartUTC, winEndUTC)

	for _, item := range lineItems {
		startDate, err1 := time.Parse("2006-01-02T15:04:05Z07:00", item.StartDate)
		endDate, err2 := time.Parse("2006-01-02T15:04:05Z07:00", item.EndDate)

		if err1 != nil || err2 != nil {
			if err1 != nil {
				log.Warnf("%s", err1)
			}
			if err2 != nil {
				log.Warnf("%s", err2)
			}
			continue
		}

		customCost := &pb.CustomCost{
			AccountName:    item.GroupName,
			ChargeCategory: "Usage",
			Description:    fmt.Sprintf("Usage for %s", item.SKU),
			ResourceName:   item.SKU,
			Id:             string(uuid.NewUUID()),
			ProviderId:     fmt.Sprintf("%s/%s/%s", item.GroupId, item.ClusterName, item.SKU),
			BilledCost:     float32(item.TotalPriceCents) / 100.0,
			ListCost:       item.Quantity * item.UnitPriceDollars,
			ListUnitPrice:  item.UnitPriceDollars,
			UsageQuantity:  item.Quantity,
			UsageUnit:      item.Unit,
		}

		if (startDate.UTC().After(winStartUTC) || startDate.UTC().Equal(winStartUTC)) &&
			(endDate.UTC().Before(winEndUTC) || endDate.UTC().Equal(winEndUTC)) {
			filteredItems = append(filteredItems, customCost)
		}
	}

	return filteredItems
}

func (p *PlanetScaleCostSource) getPlanetScaleCostsForWindow(win *opencost.Window, lineItems []LineItem) *pb.CustomCostResponse {
	costsInWindow := filterLineItemsByWindow(win, lineItems)

	resp := pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v1"},
		CostSource: "data_storage",
		Domain:     "planetscale",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      costsInWindow,
	}
	return &resp
}

func GetPendingInvoices(org string, client HTTPClient) ([]LineItem, error) {
	request, _ := http.NewRequest("GET", fmt.Sprintf(costExplorerPendingInvoicesURL, org), nil)

	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		msg := fmt.Sprintf("getPendingInvoices: error from server: %v", err)
		log.Errorf(msg)
		return nil, fmt.Errorf(msg)
	}

	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	log.Debugf("response Body: %s", string(body))

	var pendingInvoicesResponse PendingInvoice
	respUnmarshalError := json.Unmarshal([]byte(body), &pendingInvoicesResponse)
	if respUnmarshalError != nil {
		msg := fmt.Sprintf("pendingInvoices: error unmarshalling response: %v", respUnmarshalError)
		log.Errorf(msg)
		return nil, fmt.Errorf(msg)
	}

	return pendingInvoicesResponse.LineItems, nil
}

// Define your PlanetScaleConfig, PendingInvoice, and LineItem structs below
type PlanetScaleConfig struct {
	OrgID      string
	PublicKey  string
	PrivateKey string
	LogLevel   string
}

type PendingInvoice struct {
	LineItems []LineItem `json:"line_items"`
}

type LineItem struct {
	GroupName      string  `json:"group_name"`
	SKU            string  `json:"sku"`
	GroupId        string  `json:"group_id"`
	ClusterName    string  `json:"cluster_name"`
	StartDate      string  `json:"start_date"`
	EndDate        string  `json:"end_date"`
	TotalPriceCents int64   `json:"total_price_cents"`
	Quantity       float64 `json:"quantity"`
	UnitPriceDollars float64 `json:"unit_price_dollars"`
	Unit           string  `json:"unit"`
}

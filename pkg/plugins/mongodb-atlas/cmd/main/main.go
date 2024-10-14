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
	atlasconfig "github.com/opencost/opencost-plugins/pkg/plugins/mongodb-atlas/config"
	atlasplugin "github.com/opencost/opencost-plugins/pkg/plugins/mongodb-atlas/plugin"
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
	MagicCookieValue: "mongodb-atlas",
}

const costExplorerPendingInvoices = "https://cloud.mongodb.com/api/atlas/v2/orgs/%s/invoices/pending"

func main() {
	log.Debug("Initializing Mongo plugin")

	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	atlasConfig, err := atlasconfig.GetAtlasConfig(configFile)
	if err != nil {
		log.Fatalf("error building Atlas config: %v", err)
	}
	log.SetLogLevel(atlasConfig.LogLevel)

	// as per https://www.mongodb.com/docs/atlas/api/atlas-admin-api-ref/,
	// atlas admin APIs have a limit of 100 requests per minute
	rateLimiter := rate.NewLimiter(1.1, 2)
	atlasCostSrc := AtlasCostSource{
		rateLimiter: rateLimiter,
		orgID:       atlasConfig.OrgID,
	}
	atlasCostSrc.atlasClient = getAtlasClient(*atlasConfig)

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &atlasCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})

}

func getAtlasClient(atlasConfig atlasconfig.AtlasConfig) HTTPClient {
	return &http.Client{
		Transport: &digest.Transport{
			Username: atlasConfig.PublicKey,
			Password: atlasConfig.PrivateKey,
		},
	}
}

// Implementation of CustomCostSource
type AtlasCostSource struct {
	orgID       string
	rateLimiter *rate.Limiter
	atlasClient HTTPClient
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

func validateRequest(req *pb.CustomCostRequest) []string {
	var errors []string
	now := time.Now()
	// 1. Check if resolution is less than a day
	if req.Resolution.AsDuration() < 24*time.Hour {
		errors = append(errors, "Resolution should be at least one day.")
	}
	// Get the start of the current month
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	// 2. Check if start time is before the start of the current month
	if req.Start.AsTime().Before(currentMonthStart) {
		errors = append(errors, "Start date cannot be before the current month. Historical costs not currently supported")
	}

	// 3. Check if end time is before the start of the current month
	if req.End.AsTime().Before(currentMonthStart) {
		errors = append(errors, "End date cannot be before the current month. Historical costs not currently supported")
	}

	return errors
}
func (a *AtlasCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
	results := []*pb.CustomCostResponse{}

	requestErrors := validateRequest(req)
	if len(requestErrors) > 0 {
		errResp := pb.CustomCostResponse{
			Errors: requestErrors,
		}
		results = append(results, &errResp)
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

	lineItems, err := GetPendingInvoices(a.orgID, a.atlasClient)

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

		log.Debugf("fetching atlas costs for window %v", target)
		result, err := a.getAtlasCostsForWindow(&target, lineItems)
		if err != nil {
			log.Errorf("error getting costs for window %v: %v", target, err)
			errResp := pb.CustomCostResponse{}
			errResp.Errors = append(errResp.Errors, fmt.Sprintf("error getting costs for window %v: %v", target, err))
			results = append(results, &errResp)
		} else {
			results = append(results, result)
		}
	}

	return results
}

func filterLineItemsByWindow(win *opencost.Window, lineItems []atlasplugin.LineItem) []*pb.CustomCost {
	var filteredItems []*pb.CustomCost

	winStartUTC := win.Start().UTC()
	winEndUTC := win.End().UTC()
	log.Debugf("Item window %s %s", winStartUTC, winEndUTC)
	// Iterate over each line item
	for _, item := range lineItems {
		// Parse StartDate and EndDate from strings to time.Time
		startDate, err1 := time.Parse("2006-01-02T15:04:05Z07:00", item.StartDate) // Assuming date format is "2006-01-02T15:04:05Z07:00"
		endDate, err2 := time.Parse("2006-01-02T15:04:05Z07:00", item.EndDate)     // Same format assumption

		if err1 != nil || err2 != nil {
			// If parsing fails, skip this item
			continue
		}
		// 	// Iterate over the UsageDetails in CostResponse
		// for _, lineItem := range pendingInvoicesResponse.LineItems {
		// Create a new pb.CustomCost for each LineItem
		//log.Debugf("Line item %v", item)
		customCost := &pb.CustomCost{

			AccountName:    item.GroupName,
			ChargeCategory: "Usage",
			Description:    fmt.Sprintf("Usage for %s", item.SKU),
			ResourceName:   item.SKU,
			Id:             string(uuid.NewUUID()),
			ProviderId:     fmt.Sprintf("%s %s %s", item.GroupId, item.ClusterName, item.SKU),
			BilledCost:     float32(item.TotalPriceCents / 100),
			ListCost:       item.Quantity * item.UnitPriceDollars,
			ListUnitPrice:  item.UnitPriceDollars,
			UsageQuantity:  item.Quantity,
			UsageUnit:      item.Unit,
		}

		log.Debugf("Line Item %s %s", startDate.UTC(), endDate.UTC())
		// Check if the item's StartDate >= win.start and EndDate <= win.end
		if (startDate.UTC().After(winStartUTC) || startDate.UTC().Equal(winStartUTC)) &&
			(endDate.UTC().Before(winEndUTC) || endDate.UTC().Equal(winEndUTC)) {
			// 	// Append the customCost pointer to the slice
			filteredItems = append(filteredItems, customCost)
		}
	}

	return filteredItems

}

func (a *AtlasCostSource) getAtlasCostsForWindow(win *opencost.Window, lineItems []atlasplugin.LineItem) (*pb.CustomCostResponse, error) {

	//filter responses between

	costsInWindow := filterLineItemsByWindow(win, lineItems)

	resp := pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v1"},
		CostSource: "data_storage",
		Domain:     "mongodb-atlas",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      costsInWindow,
	}
	return &resp, nil
}

func GetPendingInvoices(org string, client HTTPClient) ([]atlasplugin.LineItem, error) {
	request, _ := http.NewRequest("GET", fmt.Sprintf(costExplorerPendingInvoices, org), nil)

	request.Header.Set("Accept", "application/vnd.atlas.2023-01-01+json")
	request.Header.Set("Content-Type", "application/vnd.atlas.2023-01-01+json")

	response, error := client.Do(request)
	if error != nil {
		msg := fmt.Sprintf("getPending Invoices: error from server: %v", error)
		log.Errorf(msg)
		return nil, fmt.Errorf(msg)

	}

	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	log.Debugf("response Body: %s", string(body))
	var pendingInvoicesResponse atlasplugin.PendingInvoice
	respUnmarshalError := json.Unmarshal([]byte(body), &pendingInvoicesResponse)
	if respUnmarshalError != nil {
		msg := fmt.Sprintf("pendingInvoices: error unmarshalling response: %v", respUnmarshalError)
		log.Errorf(msg)
		return nil, fmt.Errorf(msg)
	}

	return pendingInvoicesResponse.LineItems, nil
}

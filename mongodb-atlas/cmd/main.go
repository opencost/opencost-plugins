package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/icholy/digest"
	commonconfig "github.com/opencost/opencost-plugins/common/config"
	atlasconfig "github.com/opencost/opencost-plugins/mongodb-atlas/config"
	atlasplugin "github.com/opencost/opencost-plugins/mongodb-atlas/plugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"
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

const costExplorerFmt = "https://cloud.mongodb.com/api/atlas/v2/orgs/%s/billing/costExplorer/usage"
const costExplorerQueryFmt = "https://cloud.mongodb.com/api/atlas/v2/orgs/%s/billing/costExplorer/usage/%s"

func main() {
	fmt.Println("Initializing Mongo plugin")

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

/*
*
splits the start and end date as monthly chunks. Each chunk should have a startdate as the first of the month
and end date as the first day of the following month.
*
*/
// GetAtlasWindows splits the start and end date into monthly chunks
func GetAtlasWindows(startDate time.Time, endDate time.Time) []opencost.Window {
	var windows []opencost.Window

	// Ensure startDate is before endDate
	if !startDate.Before(endDate) {
		return windows
	}

	currentStart := startDate

	for currentStart.Before(endDate) {
		// Calculate the start of the next month
		nextMonth := currentStart.AddDate(0, 1, 0)

		// Add the window from the current month start to the next month start
		start := currentStart // Take address of currentStart
		end := nextMonth      // Take address of nextMonth
		var aWindow = opencost.NewWindow(&start, &end)
		windows = append(windows, aWindow)

		// Move to the start of the next month
		currentStart = nextMonth
	}

	return windows
}

func (a *AtlasCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
	results := []*pb.CustomCostResponse{}
	//MongoDB atlas needs dates as start of date of each month and calculated monthly

	// Find the first day of the month
	firstOfMonthOfStartDate := time.Date(req.Start.AsTime().Year(), req.Start.AsTime().Month(),
		1, 0, 0, 0, 0, req.Start.AsTime().Location())
	nextMonthAfterEnd := time.Date(req.End.AsTime().Year(), req.End.AsTime().Month(), 1, 0, 0, 0, 0, req.End.AsTime().Location()).AddDate(0, 1, 0)

	//TODO divide up windows into monthly pieces
	targets := GetAtlasWindows(firstOfMonthOfStartDate, nextMonthAfterEnd)
	//targets, err := opencost.GetWindows(req.Start.AsTime(), req.End.AsTime(), req.Resolution.AsDuration())
	// if err != nil {
	// 	log.Errorf("error getting windows: %v", err)
	// 	errResp := pb.CustomCostResponse{
	// 		Errors: []string{fmt.Sprintf("error getting windows: %v", err)},
	// 	}
	// 	results = append(results, &errResp)
	// 	return results
	// }

	for _, target := range targets {
		if target.Start().After(time.Now().UTC()) || target.End().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		log.Debugf("fetching atlas costs for window %v", target)
		result, err := a.getAtlasCostsForWindow(&target)
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

func (a *AtlasCostSource) getAtlasCostsForWindow(win *opencost.Window) (*pb.CustomCostResponse, error) {

	// get the token
	token, err := CreateCostExplorerQueryToken(a.orgID, *win.Start(), *win.End(), a.atlasClient)
	if err != nil {
		log.Errorf("error getting token: %v", err)
		return nil, err
	}

	// get the costs
	costs, err := GetCosts(a.atlasClient, a.orgID, token)
	if err != nil {
		log.Errorf("error getting costs: %v", err)
		return nil, err
	}

	resp := pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v1"},
		CostSource: "data_storage",
		Domain:     "mongodb-atlas",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      costs,
	}
	return &resp, nil
}

func GetCosts(client HTTPClient, org string, token string) ([]*pb.CustomCost, error) {
	request, _ := http.NewRequest("GET", fmt.Sprintf(costExplorerQueryFmt, org, token), nil)

	request.Header.Set("Accept", "application/vnd.atlas.2023-01-01+json")
	request.Header.Set("Content-Type", "application/vnd.atlas.2023-01-01+json")

	response, error := client.Do(request)
	statusCode := response.StatusCode
	//102 status code means processing - so repeat call 2 times to see if we get a response
	for count := 1; count < 2 && statusCode == http.StatusProcessing; count++ {
		// Sleep for 5 seconds before the next request
		time.Sleep(5 * time.Second)
		response, _ := client.Do(request)
		statusCode = response.StatusCode

	}

	if statusCode == http.StatusProcessing {
		msg := "timeout waiting for response"
		return nil, fmt.Errorf(msg)
	}
	if error != nil {
		msg := fmt.Sprintf("getCostExplorerUsage: error from server: %v", error)
		log.Errorf(msg)
		return nil, fmt.Errorf(msg)

	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	log.Debugf("response Body: %s", string(body))
	var costResponse atlasplugin.CostResponse
	respUnmarshalError := json.Unmarshal([]byte(body), &costResponse)
	if respUnmarshalError != nil {
		msg := fmt.Sprintf("getCost: error unmarshalling response: %v", respUnmarshalError)
		log.Errorf(msg)
		return nil, fmt.Errorf(msg)
	}
	var costs []*pb.CustomCost
	// Iterate over the UsageDetails in CostResponse
	for _, invoice := range costResponse.UsageDetails {
		// Create a new pb.CustomCost for each Invoice
		customCost := &pb.CustomCost{
			Id:             invoice.InvoiceId,
			AccountName:    invoice.OrganizationName,
			ChargeCategory: invoice.Service,
			BilledCost:     invoice.UsageAmount,
		}

		// Append the customCost pointer to the slice
		costs = append(costs, customCost)
	}
	return costs, nil
}

// pass list of orgs , start date, end date
func CreateCostExplorerQueryToken(org string, startDate time.Time, endDate time.Time,
	client HTTPClient) (string, error) {
	// Define the layout for the desired format
	layout := "2006-01-02"

	// Convert the time.Time object to a string in yyyy-mm-dd format
	startDateString := startDate.Format(layout)
	endDateString := endDate.Format(layout)

	payload := atlasplugin.CreateCostExplorerQueryPayload{

		EndDate:       endDateString,
		StartDate:     startDateString,
		Organizations: []string{org},
	}
	payloadJson, _ := json.Marshal(payload)

	request, _ := http.NewRequest("POST", fmt.Sprintf(costExplorerFmt, org), bytes.NewBuffer(payloadJson))

	request.Header.Set("Accept", "application/vnd.atlas.2023-01-01+json")
	request.Header.Set("Content-Type", "application/vnd.atlas.2023-01-01+json")

	response, error := client.Do(request)
	if error != nil {
		msg := fmt.Sprintf("createCostExplorerQueryToken: error from server: %v", error)
		log.Errorf(msg)
		return "", fmt.Errorf(msg)

	}
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)
	log.Debugf("response Body: %s", string(body))
	var createCostExplorerQueryResponse atlasplugin.CreateCostExplorerQueryResponse
	respUnmarshalError := json.Unmarshal([]byte(body), &createCostExplorerQueryResponse)
	if respUnmarshalError != nil {
		msg := fmt.Sprintf("createCostExplorerQueryToken: error unmarshalling response: %v", respUnmarshalError)
		log.Errorf(msg)
		return "", fmt.Errorf(msg)
	}
	return createCostExplorerQueryResponse.Token, nil
}

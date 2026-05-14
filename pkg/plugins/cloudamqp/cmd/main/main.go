package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/go-plugin"
	commonconfig "github.com/opencost/opencost-plugins/common/config"
	cloudamqpplugin "github.com/opencost/opencost-plugins/pkg/plugins/cloudamqp/cloudamqpplugin"
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
	MagicCookieValue: "cloudamqp",
}

const (
	cloudamqpBaseURL      = "https://customer.cloudamqp.com/api"
	instancesEndpoint     = "/instances"
	invoicesEndpoint      = "/invoices"
)

func main() {
	log.Debug("Initializing CloudAMQP plugin")

	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	cloudamqpConfig, err := cloudamqpplugin.GetCloudAMQPConfig(configFile)
	if err != nil {
		log.Fatalf("error building CloudAMQP config: %v", err)
	}
	log.SetLogLevel(cloudamqpConfig.LogLevel)

	// CloudAMQP API rate limit: be conservative
	rateLimiter := rate.NewLimiter(1.0, 2)
	cloudamqpCostSrc := CloudAMQPCostSource{
		rateLimiter: rateLimiter,
		apiKey:      cloudamqpConfig.APIKey,
		accountID:   cloudamqpConfig.AccountID,
		httpClient:  &http.Client{},
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &cloudamqpCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

// HTTPClient interface for testability
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Implementation of CustomCostSource
type CloudAMQPCostSource struct {
	apiKey      string
	accountID   string
	rateLimiter *rate.Limiter
	httpClient  HTTPClient
}

func (c *CloudAMQPCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
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

	// Fetch invoices from CloudAMQP
	invoices, err := c.getInvoices()
	if err != nil {
		log.Errorf("error fetching CloudAMQP invoices: %v", err)
		errResp := pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error fetching CloudAMQP invoices: %v", err)},
		}
		results = append(results, &errResp)
		return results
	}

	// Fetch instances for enrichment
	instances, err := c.getInstances()
	if err != nil {
		log.Warnf("error fetching CloudAMQP instances, proceeding without enrichment: %v", err)
		instances = []cloudamqpplugin.CloudAMQPInstance{}
	}

	instanceMap := makeInstanceMap(instances)

	for _, target := range targets {
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		log.Debugf("fetching CloudAMQP costs for window %v", target)
		result := c.getCloudAMQPCostsForWindow(&target, invoices, instanceMap)
		results = append(results, result)
	}

	return results
}

func (c *CloudAMQPCostSource) getInvoices() ([]cloudamqpplugin.CloudAMQPInvoice, error) {
	url := fmt.Sprintf("%s%s", cloudamqpBaseURL, invoicesEndpoint)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	req.Header.Set("Content-Type", "application/json")

	if err := c.rateLimiter.Wait(context.Background()); err != nil {
		return nil, fmt.Errorf("rate limiter error: %v", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var invoices []cloudamqpplugin.CloudAMQPInvoice
	if err := json.Unmarshal(body, &invoices); err != nil {
		return nil, fmt.Errorf("error unmarshalling invoices: %v", err)
	}

	return invoices, nil
}

func (c *CloudAMQPCostSource) getInstances() ([]cloudamqpplugin.CloudAMQPInstance, error) {
	url := fmt.Sprintf("%s%s", cloudamqpBaseURL, instancesEndpoint)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	req.Header.Set("Content-Type", "application/json")

	if err := c.rateLimiter.Wait(context.Background()); err != nil {
		return nil, fmt.Errorf("rate limiter error: %v", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var instances []cloudamqpplugin.CloudAMQPInstance
	if err := json.Unmarshal(body, &instances); err != nil {
		return nil, fmt.Errorf("error unmarshalling instances: %v", err)
	}

	return instances, nil
}

func makeInstanceMap(instances []cloudamqpplugin.CloudAMQPInstance) map[int]cloudamqpplugin.CloudAMQPInstance {
	m := make(map[int]cloudamqpplugin.CloudAMQPInstance)
	for _, inst := range instances {
		m[inst.ID] = inst
	}
	return m
}

func (c *CloudAMQPCostSource) getCloudAMQPCostsForWindow(
	win *opencost.Window,
	invoices []cloudamqpplugin.CloudAMQPInvoice,
	instanceMap map[int]cloudamqpplugin.CloudAMQPInstance,
) *pb.CustomCostResponse {
	costs := []*pb.CustomCost{}

	winStartUTC := win.Start().UTC()
	winEndUTC := win.End().UTC()

	for _, invoice := range invoices {
		for _, item := range invoice.Items {
			// Parse the item period dates
			itemStart, err1 := time.Parse(time.RFC3339, item.PeriodStart)
			itemEnd, err2 := time.Parse(time.RFC3339, item.PeriodStart)

			// Try alternative date formats if RFC3339 fails
			if err1 != nil {
				itemStart, err1 = time.Parse("2006-01-02", item.PeriodStart)
			}
			if err2 != nil {
				itemEnd, err2 = time.Parse("2006-01-02", item.PeriodEnd)
			}

			if err1 != nil || err2 != nil {
				// If date parsing fails, include the item anyway (use invoice dates as fallback)
				log.Debugf("could not parse item dates, including without date filtering: %s - %s", item.PeriodStart, item.PeriodEnd)
				itemStart = winStartUTC
				itemEnd = winEndUTC
			}

			// Check if the item falls within the window
			if (itemStart.UTC().After(winStartUTC) || itemStart.UTC().Equal(winStartUTC)) &&
				(itemEnd.UTC().Before(winEndUTC) || itemEnd.UTC().Equal(winEndUTC)) {

				// Enrich with instance info if available
				region := "unknown"
				instanceName := item.InstanceName
				if inst, ok := instanceMap[item.InstanceID]; ok {
					region = inst.Region
					if instanceName == "" {
						instanceName = inst.Name
					}
				}

				providerID := fmt.Sprintf("cloudamqp/%d/%s", item.InstanceID, item.Plan)
				cost := &pb.CustomCost{
					Zone:           region,
					AccountName:    instanceName,
					ChargeCategory: "Usage",
					Description:    item.Description,
					ResourceName:   item.Plan,
					ResourceType:   "message_queue",
					Id:             string(uuid.NewUUID()),
					ProviderId:     providerID,
					BilledCost:     float32(item.AmountCents) / 100.0,
					ListCost:       float32(item.AmountCents) / 100.0,
					ListUnitPrice:  float32(item.AmountCents) / 100.0 / item.Quantity,
					UsageQuantity:  item.Quantity,
					UsageUnit:      item.Unit,
					Labels:         map[string]string{},
				}
				costs = append(costs, cost)
			}
		}
	}

	resp := pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v1"},
		CostSource: "message_queue",
		Domain:     "cloudamqp",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      costs,
	}
	return &resp
}

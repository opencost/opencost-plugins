package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-plugin"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	cloudamqpconfig "github.com/opencost/opencost-plugins/pkg/plugins/cloudamqp/config"
	cloudamqpplugin "github.com/opencost/opencost-plugins/pkg/plugins/cloudamqp/cloudamqpplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
)

var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "cloudamqp",
}

const cloudamqpAPIBase = "https://customer.cloudamqp.com/api"
const cloudamqpInstancesURL = cloudamqpAPIBase + "/instances"
const cloudamqpInvoicesURL = cloudamqpAPIBase + "/invoices"

type CloudAMQPCostSource struct {
	rateLimiter *rate.Limiter
	config      *cloudamqpconfig.CloudAMQPConfig
	client      HTTPClient
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
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

	for _, target := range targets {
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		log.Debugf("fetching CloudAMQP costs for window %v", target)
		result := c.getCloudAMQPCostsForWindow(target)
		results = append(results, result)
	}

	return results
}

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	cloudamqpCfg, err := cloudamqpconfig.GetCloudAMQPConfig(configFile)
	if err != nil {
		log.Fatalf("error building CloudAMQP config: %v", err)
	}
	log.SetLogLevel(cloudamqpCfg.LogLevel)

	rateLimiter := rate.NewLimiter(1, 2)
	cloudamqpCostSrc := CloudAMQPCostSource{
		rateLimiter: rateLimiter,
		config:      cloudamqpCfg,
		client:      &http.Client{},
	}

	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &cloudamqpCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

func boilerplateCloudAMQPCustomCost(win opencost.Window) pb.CustomCostResponse {
	return pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v1"},
		CostSource: "message_queue",
		Domain:     "cloudamqp",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      []*pb.CustomCost{},
	}
}

func (c *CloudAMQPCostSource) getCloudAMQPCostsForWindow(window opencost.Window) *pb.CustomCostResponse {
	ccResp := boilerplateCloudAMQPCustomCost(window)

	// Try to fetch invoices first
	invoices, err := c.getInvoices()
	if err != nil {
		// If invoices API fails, fall back to instance-based pricing
		log.Debugf("could not fetch invoices, falling back to instance pricing: %v", err)
		instances, err := c.getInstances()
		if err != nil {
			ccResp.Errors = append(ccResp.Errors, fmt.Sprintf("error getting CloudAMQP data: %v", err))
			return &ccResp
		}
		ccResp.Costs = c.instancesToCustomCosts(invoices, instances)
		return &ccResp
	}

	// If we have invoices, use those
	if len(invoices) > 0 {
		ccResp.Costs = c.invoicesToCustomCosts(invoices)
		return &ccResp
	}

	// Fall back to instance-based pricing
	instances, err := c.getInstances()
	if err != nil {
		ccResp.Errors = append(ccResp.Errors, fmt.Sprintf("error getting CloudAMQP instances: %v", err))
		return &ccResp
	}
	ccResp.Costs = c.instancesToCustomCosts(nil, instances)
	return &ccResp
}

func (c *CloudAMQPCostSource) instancesToCustomCosts(invoices []cloudamqpplugin.CloudAMQPInvoice, instances []cloudamqpplugin.CloudAMQPInstance) []*pb.CustomCost {
	customCosts := []*pb.CustomCost{}
	for _, instance := range instances {
		customCost := pb.CustomCost{
			AccountName:    instance.Name,
			ChargeCategory: "Recurring",
			Description:    fmt.Sprintf("CloudAMQP %s (%s) - %d nodes", instance.Plan, instance.Region, instance.Nodes),
			ResourceName:   instance.Name,
			ResourceType:   instance.Plan,
			Id:             uuid.New().String(),
			ProviderId:     fmt.Sprintf("%d/%s", instance.ID, instance.Plan),
			BilledCost:     float32(instance.Price.Amount),
			ListCost:       float32(instance.Price.Amount),
			UsageQuantity:  1,
			UsageUnit:      instance.Price.Interval,
			Labels:         map[string]string{},
		}

		if instance.Region != "" {
			customCost.Labels["region"] = instance.Region
		}
		if instance.Nodes > 0 {
			customCost.Labels["nodes"] = fmt.Sprintf("%d", instance.Nodes)
		}
		if instance.Price.Currency != "" {
			customCost.Labels["currency"] = instance.Price.Currency
		}
		if instance.NodeType != "" {
			customCost.Labels["node_type"] = instance.NodeType
		}

		customCosts = append(customCosts, &customCost)
	}
	return customCosts
}

func (c *CloudAMQPCostSource) invoicesToCustomCosts(invoices []cloudamqpplugin.CloudAMQPInvoice) []*pb.CustomCost {
	customCosts := []*pb.CustomCost{}
	for _, invoice := range invoices {
		for _, item := range invoice.Items {
			customCost := pb.CustomCost{
				AccountName:    item.InstanceName,
				ChargeCategory: "Usage",
				Description:    item.Description,
				ResourceName:   item.InstanceName,
				ResourceType:   item.Plan,
				Id:             uuid.New().String(),
				ProviderId:     fmt.Sprintf("%d/%s", item.InstanceID, item.Plan),
				BilledCost:     float32(item.Amount),
				ListCost:       float32(item.Amount),
				UsageQuantity:  float32(item.Quantity),
				Labels:         map[string]string{},
			}

			if item.PeriodStart != "" {
				customCost.Labels["period_start"] = item.PeriodStart
			}
			if item.PeriodEnd != "" {
				customCost.Labels["period_end"] = item.PeriodEnd
			}

			customCosts = append(customCosts, &customCost)
		}
	}
	return customCosts
}

func (c *CloudAMQPCostSource) getInstances() ([]cloudamqpplugin.CloudAMQPInstance, error) {
	req, err := http.NewRequest("GET", cloudamqpInstancesURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	req.SetBasicAuth("", c.config.APIKey)
	req.Header.Set("Accept", "application/json")

	err = c.rateLimiter.Wait(context.Background())
	if err != nil {
		return nil, fmt.Errorf("rate limiter error: %v", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request to CloudAMQP API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("CloudAMQP API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var instances []cloudamqpplugin.CloudAMQPInstance
	err = json.Unmarshal(body, &instances)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling CloudAMQP instances response: %v", err)
	}

	return instances, nil
}

func (c *CloudAMQPCostSource) getInvoices() ([]cloudamqpplugin.CloudAMQPInvoice, error) {
	req, err := http.NewRequest("GET", cloudamqpInvoicesURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	req.SetBasicAuth("", c.config.APIKey)
	req.Header.Set("Accept", "application/json")

	err = c.rateLimiter.Wait(context.Background())
	if err != nil {
		return nil, fmt.Errorf("rate limiter error: %v", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request to CloudAMQP invoices API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("CloudAMQP invoices API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var invoices []cloudamqpplugin.CloudAMQPInvoice
	err = json.Unmarshal(body, &invoices)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling CloudAMQP invoices response: %v", err)
	}

	return invoices, nil
}

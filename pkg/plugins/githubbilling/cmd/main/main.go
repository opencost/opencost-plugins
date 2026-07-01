package main

import (
	"context"
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
	githubbillingconfig "github.com/opencost/opencost-plugins/pkg/plugins/githubbilling/config"
	githubbillingplugin "github.com/opencost/opencost-plugins/pkg/plugins/githubbilling/githubbillingplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
)

var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "githubbilling",
}

const githubBillingUsageURL = "https://api.github.com/organizations/%s/settings/billing/usage"
const githubBillingPremiumURL = "https://api.github.com/organizations/%s/settings/billing/premium_request/usage"
const githubAPIVersion = "2022-11-28"

type GitHubBillingCostSource struct {
	rateLimiter *rate.Limiter
	config      *githubbillingconfig.GitHubBillingConfig
	client      HTTPClient
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

func (g *GitHubBillingCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
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

		log.Debugf("fetching GitHub billing costs for window %v", target)
		result := g.getGitHubBillingCostsForWindow(target)
		results = append(results, result)
	}

	return results
}

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	githubCfg, err := githubbillingconfig.GetGitHubBillingConfig(configFile)
	if err != nil {
		log.Fatalf("error building GitHub Billing config: %v", err)
	}
	log.SetLogLevel(githubCfg.LogLevel)

	// GitHub API rate limit: 5000 requests per hour for authenticated users
	rateLimiter := rate.NewLimiter(1, 3)
	githubCostSrc := GitHubBillingCostSource{
		rateLimiter: rateLimiter,
		config:      githubCfg,
		client:      &http.Client{},
	}

	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &githubCostSrc},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}

func boilerplateGitHubBillingCustomCost(win opencost.Window) pb.CustomCostResponse {
	return pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v1"},
		CostSource: "dev_tools",
		Domain:     "github",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      []*pb.CustomCost{},
	}
}

func (g *GitHubBillingCostSource) getGitHubBillingCostsForWindow(window opencost.Window) *pb.CustomCostResponse {
	ccResp := boilerplateGitHubBillingCustomCost(window)

	year := window.Start().Year()
	month := int(window.Start().Month())

	// Fetch standard usage billing
	usageItems, err := g.getUsageBilling(year, month)
	if err != nil {
		ccResp.Errors = append(ccResp.Errors, fmt.Sprintf("error getting GitHub usage billing: %v", err))
		return &ccResp
	}

	// Fetch premium request usage billing
	premiumItems, err := g.getPremiumUsageBilling(year, month)
	if err != nil {
		// Premium usage may not be available for all orgs, log but don't fail
		log.Debugf("could not fetch premium usage billing: %v", err)
	}

	customCosts := []*pb.CustomCost{}

	// Process standard usage items
	for _, item := range usageItems {
		customCost := pb.CustomCost{
			AccountName:    item.OrganizationName,
			ChargeCategory: "Usage",
			Description:    fmt.Sprintf("GitHub %s (%s)", item.Product, item.SKU),
			ResourceName:   item.Product,
			ResourceType:   item.SKU,
			Id:             uuid.New().String(),
			ProviderId:     fmt.Sprintf("%s/%s/%s", item.OrganizationName, item.Product, item.SKU),
			BilledCost:     float32(item.NetAmount),
			ListCost:       float32(item.GrossAmount),
			ListUnitPrice:  float32(item.PricePerUnit),
			UsageQuantity:  float32(item.Quantity),
			UsageUnit:      item.UnitType,
			Labels:         map[string]string{},
		}

		if item.RepositoryName != "" {
			customCost.Labels["repository_name"] = item.RepositoryName
		}
		if item.Date != "" {
			customCost.Labels["date"] = item.Date
		}
		if item.DiscountAmount > 0 {
			customCost.Labels["discount_amount"] = fmt.Sprintf("%.6f", item.DiscountAmount)
		}

		customCosts = append(customCosts, &customCost)
	}

	// Process premium usage items
	for _, item := range premiumItems {
		customCost := pb.CustomCost{
			AccountName:    g.config.Organization,
			ChargeCategory: "Usage",
			Description:    fmt.Sprintf("GitHub Premium %s (%s)", item.Product, item.Model),
			ResourceName:   item.Product,
			ResourceType:   item.SKU,
			Id:             uuid.New().String(),
			ProviderId:     fmt.Sprintf("%s/%s/%s", g.config.Organization, item.Product, item.Model),
			BilledCost:     float32(item.NetAmount),
			ListCost:       float32(item.GrossAmount),
			ListUnitPrice:  float32(item.PricePerUnit),
			UsageQuantity:  float32(item.NetQuantity),
			UsageUnit:      item.UnitType,
			Labels:         map[string]string{},
		}

		if item.Model != "" {
			customCost.Labels["model"] = item.Model
		}
		if item.DiscountAmount > 0 {
			customCost.Labels["discount_amount"] = fmt.Sprintf("%.6f", item.DiscountAmount)
		}

		customCosts = append(customCosts, &customCost)
	}

	ccResp.Costs = customCosts
	return &ccResp
}

func (g *GitHubBillingCostSource) getUsageBilling(year, month int) ([]githubbillingplugin.GitHubBillingUsageItem, error) {
	url := fmt.Sprintf(githubBillingUsageURL, g.config.Organization)
	url = fmt.Sprintf("%s?year=%d&month=%d", url, year, month)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", g.config.APIToken))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)

	err = g.rateLimiter.Wait(context.Background())
	if err != nil {
		return nil, fmt.Errorf("rate limiter error: %v", err)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request to GitHub Billing API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub Billing API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var usageResp githubbillingplugin.GitHubBillingUsageResponse
	err = json.Unmarshal(body, &usageResp)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling GitHub Billing response: %v", err)
	}

	return usageResp.UsageItems, nil
}

func (g *GitHubBillingCostSource) getPremiumUsageBilling(year, month int) ([]githubbillingplugin.GitHubBillingPremiumUsageItem, error) {
	url := fmt.Sprintf(githubBillingPremiumURL, g.config.Organization)
	url = fmt.Sprintf("%s?year=%d&month=%d", url, year, month)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", g.config.APIToken))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)

	err = g.rateLimiter.Wait(context.Background())
	if err != nil {
		return nil, fmt.Errorf("rate limiter error: %v", err)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request to GitHub Premium Billing API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub Premium Billing API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var premiumResp githubbillingplugin.GitHubBillingPremiumUsageResponse
	err = json.Unmarshal(body, &premiumResp)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling GitHub Premium Billing response: %v", err)
	}

	return premiumResp.UsageItems, nil
}

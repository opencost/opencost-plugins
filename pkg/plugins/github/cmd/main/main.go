package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-plugin"
	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	githubplugin "github.com/opencost/opencost-plugins/pkg/plugins/github/githubplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "github",
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type GitHubCostSource struct {
	client HTTPClient
	config *githubplugin.GitHubConfig
}

type usageReport struct {
	UsageItems []usageItem `json:"usageItems"`
}

type usageItem struct {
	Date             string  `json:"date"`
	Product          string  `json:"product"`
	SKU              string  `json:"sku"`
	Quantity         float64 `json:"quantity"`
	UnitType         string  `json:"unitType"`
	PricePerUnit     float64 `json:"pricePerUnit"`
	GrossAmount      float64 `json:"grossAmount"`
	DiscountAmount   float64 `json:"discountAmount"`
	NetAmount        float64 `json:"netAmount"`
	OrganizationName string  `json:"organizationName"`
	RepositoryName   string  `json:"repositoryName"`
}

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	githubConfig, err := githubplugin.GetGitHubConfig(configFile)
	if err != nil {
		log.Fatalf("error building GitHub config: %v", err)
	}
	log.SetLogLevel(githubConfig.LogLevel)

	githubCostSrc := GitHubCostSource{
		client: http.DefaultClient,
		config: githubConfig,
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

func (g *GitHubCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
	results := []*pb.CustomCostResponse{}

	targets, err := opencost.GetWindows(req.Start.AsTime(), req.End.AsTime(), req.Resolution.AsDuration())
	if err != nil {
		return append(results, &pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting windows: %v", err)},
		})
	}

	if req.Resolution.AsDuration() != timeutil.Day {
		return append(results, &pb.CustomCostResponse{
			Errors: []string{"github plugin only supports daily resolution"},
		})
	}

	for _, target := range targets {
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		result := boilerplateGitHubCustomCost(target)
		report, err := g.getUsageReportForWindow(target)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("error getting GitHub usage report: %v", err))
			results = append(results, &result)
			continue
		}

		result.Costs = githubUsageItemsToCustomCosts(g.config.Account, report.UsageItems)
		results = append(results, &result)
	}

	return results
}

func boilerplateGitHubCustomCost(win opencost.Window) pb.CustomCostResponse {
	return pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v1"},
		CostSource: "SaaS",
		Domain:     "github",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      []*pb.CustomCost{},
	}
}

func (g *GitHubCostSource) getUsageReportForWindow(window opencost.Window) (*usageReport, error) {
	endpoint, err := g.usageURLForWindow(window)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating GitHub usage request: %v", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", g.config.Token))
	req.Header.Set("X-GitHub-Api-Version", g.config.APIVersion)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error doing GitHub usage request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		bodyString := "<empty>"
		if readErr == nil {
			bodyString = string(bodyBytes)
		}
		return nil, fmt.Errorf("received non-200 response for GitHub usage request: %d body: %s", resp.StatusCode, bodyString)
	}

	var report usageReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return nil, fmt.Errorf("error decoding GitHub usage response: %v", err)
	}

	return &report, nil
}

func (g *GitHubCostSource) usageURLForWindow(window opencost.Window) (string, error) {
	baseURL, err := url.Parse(strings.TrimRight(g.config.APIBaseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("invalid GitHub API base URL: %v", err)
	}

	account := url.PathEscape(g.config.Account)
	if g.config.AccountType == githubplugin.AccountTypeUser {
		baseURL.Path = fmt.Sprintf("%s/users/%s/settings/billing/usage", baseURL.Path, account)
	} else {
		baseURL.Path = fmt.Sprintf("%s/organizations/%s/settings/billing/usage", baseURL.Path, account)
	}

	start := window.Start().UTC()
	query := baseURL.Query()
	query.Set("year", fmt.Sprintf("%d", start.Year()))
	query.Set("month", fmt.Sprintf("%d", int(start.Month())))
	query.Set("day", fmt.Sprintf("%d", start.Day()))
	baseURL.RawQuery = query.Encode()

	return baseURL.String(), nil
}

func githubUsageItemsToCustomCosts(account string, usageItems []usageItem) []*pb.CustomCost {
	customCosts := []*pb.CustomCost{}
	for _, item := range usageItems {
		resourceName := item.SKU
		if resourceName == "" {
			resourceName = item.Product
		}

		accountName := item.OrganizationName
		if accountName == "" {
			accountName = account
		}

		providerIDParts := []string{accountName, item.RepositoryName, item.Product, item.SKU, item.Date}
		providerID := strings.Join(compact(providerIDParts), "/")

		customCost := pb.CustomCost{
			BilledCost:     float32(item.NetAmount),
			ListCost:       float32(item.GrossAmount),
			AccountName:    accountName,
			ChargeCategory: "Usage",
			Description:    fmt.Sprintf("GitHub %s usage for %s", item.Product, resourceName),
			ResourceName:   resourceName,
			ResourceType:   item.Product,
			Id:             uuid.New().String(),
			ProviderId:     providerID,
			UsageQuantity:  float32(item.Quantity),
			UsageUnit:      item.UnitType,
		}

		customCosts = append(customCosts, &customCost)
	}

	return customCosts
}

func compact(values []string) []string {
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

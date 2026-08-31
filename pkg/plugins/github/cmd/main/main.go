package main

import (
	"context"
	"encoding/json"
	"fmt"
	// "io"
	// "net/http"
	// "regexp"
	// "strconv"
	// "strings"
	// "time"

	"os"

	"github.com/hashicorp/go-plugin"
	"github.com/google/go-github/v74/github"
	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	githubplugin "github.com/opencost/opencost-plugins/pkg/plugins/github/githubplugin"
	"golang.org/x/time/rate"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
)

const organisation = "opencost"

var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "github",
}

type githubSource struct {
	ghCtx context.Context
	usageApi *github.BillingService
	rateLimiter *rate.Limiter
}

func (g *githubSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
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

	// This only works for whole number dates
	for _, target := range targets {
		fmt.Println("Calling Billing Or", target)
		windowPointer := target.Start()
		year := int((*windowPointer).Year())

		month := int((*windowPointer).Month())

		day := int((*windowPointer).Day())

		hour := int((*windowPointer).Hour())

		// Check for resolution level, it can be hour, day, month, year
		usageReportOptions := &github.UsageReportOptions{
			Year: &year,
			Month: &month,
			Day: &day,
			Hour: &hour,
		}

		fmt.Println("Calling Billing Or")
		actionsPricing, err := g.scrapeBillingOrg("cloudrainsdev", usageReportOptions)
		// actionsPricing, _, err = g.scrapeBillingUser(user, usageReportOptions)

		if err != nil {
			log.Errorf("error getting dd pricing: %v", err)
			errResp := pb.CustomCostResponse{
				Errors: []string{fmt.Sprintf("error getting dd pricing: %v", err)},
			}
			results = append(results, &errResp)
			return results
		} else {
			log.Debugf("got list pricing: %v", &actionsPricing.UsageItems[0].Quantity)
		}
	}
	return results
}


func (g *githubSource) scrapeBillingOrg(organisation string, usageReportOptions *github.UsageReportOptions) (*github.UsageReport, error) {
	orgBilling, _, err := g.usageApi.GetUsageReportOrg(g.ghCtx, organisation, usageReportOptions)
	// Check for errors
	// if response.StatusCode != http.StatusOk {
	// 	return nil, fmt.Errorf("failed to retrieve actions price. Status code: %d", response.StatusCode)
	// }

	if err != nil {
		return nil, fmt.Errorf("failed to fetch actions price: %v", err)
	}
	return orgBilling, nil
}

func (g *githubSource) scrapeBillingUser(user string, usageReportOptions *github.UsageReportOptions) (*github.UsageReport, error) {
	userBilling, _, err := g.usageApi.GetUsageReportUser(g.ghCtx, user, usageReportOptions)
	// Check for errors
	// if response.StatusCode != http.StatusOk {
	// 	return nil, fmt.Errorf("failed to retrieve actions price. Status code: %d", response.StatusCode)
	// }

	if err != nil {
		return nil, fmt.Errorf("failed to fetch actions price: %v", err)
	}
	return userBilling, nil
}


func getConfigFilePath() (string, error) {
	// plugins expect exactly 2 args: the executable itself,
	// and a path to the config file to use
	// all config for the plugin must come through the config file
	if len(os.Args) != 2 {
		return "", fmt.Errorf("plugins require 2 args: the plugin itself, and the full path to its config file. Got %d args", len(os.Args))
	}

	_, err := os.Stat(os.Args[1])
	if err != nil {
		return "", fmt.Errorf("error reading config file at %s: %v", os.Args[1], err)
	}

	return os.Args[1], nil
}

func getGithubConfig(configFilePath string) (*githubplugin.GithubConfig, error) {
	var result githubplugin.GithubConfig
	bytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file for DD config @ %s: %v", configFilePath, err)
	}
	err = json.Unmarshal(bytes, &result)
	if err != nil {
		return nil, fmt.Errorf("error marshaling json into DD config %v", err)
	}

	// if result.DDLogLevel == "" {
	// 	result.DDLogLevel = "info"
	// }

	return &result, nil
}

func getGithubClients(ghConfig githubplugin.GithubConfig) (context.Context, *github.BillingService) {
	ghCtx := context.Background()
	ghClient := github.NewClient(nil).WithAuthToken(ghConfig.GithubPAT).Billing
	return ghCtx, ghClient
}

func main() {
	
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	ghConfig, err := getGithubConfig(configFile)
	if err != nil {
		log.Fatalf("error building DD config: %v", err)
	}
	// set log level
	rateLimiter := rate.NewLimiter(0.25, 5)
	
	ghCostSrc := githubSource{
		rateLimiter: rateLimiter,
	}

	ghCostSrc.ghCtx, ghCostSrc.usageApi = getGithubClients(*ghConfig)

	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &ghCostSrc},
	}
	
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}


package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	datadogplugin "github.com/opencost/opencost-plugins/pkg/plugins/datadog/datadogplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPricingFetch(t *testing.T) {
	listPricing, err := scrapeDatadogPrices(url)
	if err != nil {
		t.Fatalf("failed to get pricing: %v", err)
	}
	fmt.Printf("got response: %v", listPricing)
	if len(listPricing.Details) == 0 {
		t.Fatalf("expected non zero pricing details")
	}
}

func TestGetCustomCosts(t *testing.T) {
	// read necessary env vars. If any are missing, log warning and skip test
	ddSite := os.Getenv("DD_SITE")
	ddApiKey := os.Getenv("DD_API_KEY")
	ddAppKey := os.Getenv("DD_APPLICATION_KEY")

	if ddSite == "" {
		log.Warnf("DD_SITE undefined, this needs to have the URL of your DD instance, skipping test")
		t.Skip()
		return
	}

	if ddApiKey == "" {
		log.Warnf("DD_API_KEY undefined, skipping test")
		t.Skip()
		return
	}

	if ddAppKey == "" {
		log.Warnf("DD_APPLICATION_KEY undefined, skipping test")
		t.Skip()
		return
	}

	// write out config to temp file using contents of env vars
	config := datadogplugin.DatadogConfig{
		DDSite:   ddSite,
		DDAPIKey: ddApiKey,
		DDAppKey: ddAppKey,
	}

	rateLimiter := rate.NewLimiter(0.25, 5)
	ddCostSrc := DatadogCostSource{
		rateLimiter: rateLimiter,
	}
	ddCostSrc.ddCtx, ddCostSrc.usageApi = getDatadogClients(config)
	windowStart := time.Date(2024, 3, 11, 0, 0, 0, 0, time.UTC)
	// query for qty 2 of 1 hour windows
	windowEnd := time.Date(2024, 3, 12, 0, 0, 0, 0, time.UTC)

	req := &pb.CustomCostRequest{
		Start:      timestamppb.New(windowStart),
		End:        timestamppb.New(windowEnd),
		Resolution: durationpb.New(timeutil.Day),
	}

	log.SetLogLevel("debug")
	resp := ddCostSrc.GetCustomCosts(req)

	if len(resp) == 0 {
		t.Fatalf("empty response")
	}
}

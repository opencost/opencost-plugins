package main

import (
	"os"
	"testing"
	"time"

	openaiplugin "github.com/opencost/opencost-plugins/pkg/plugins/openai/openaiplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/util/timeutil"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestGetCustomCosts(t *testing.T) {
	// read necessary env vars. If any are missing, log warning and skip test
	oaiApiKey := os.Getenv("OAI_API_KEY")
	if oaiApiKey == "" {
		log.Warnf("OAI_API_KEY undefined, skipping test")
		t.Skip()
		return
	}

	//set up config
	config := openaiplugin.OpenAIConfig{
		APIKey: oaiApiKey,
	}

	rateLimiter := rate.NewLimiter(1, 5)
	oaiCostSrc := OpenAICostSource{
		rateLimiter: rateLimiter,
		config:      &config,
	}

	windowStart := time.Date(2024, 10, 9, 0, 0, 0, 0, time.UTC)
	// query for qty 2 of 1 hour windows
	windowEnd := time.Date(2024, 10, 10, 0, 0, 0, 0, time.UTC)

	req := &pb.CustomCostRequest{
		Start:      timestamppb.New(windowStart),
		End:        timestamppb.New(windowEnd),
		Resolution: durationpb.New(timeutil.Day),
	}

	log.SetLogLevel("debug")
	resp := oaiCostSrc.GetCustomCosts(req)

	if len(resp) == 0 {
		t.Fatalf("empty response")
	}
}

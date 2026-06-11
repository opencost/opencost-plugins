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

	windowEnd := time.Now().UTC().AddDate(0, -1, 0).Truncate(24 * time.Hour)
	windowStart := windowEnd.AddDate(0, 0, -1)

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

	for _, r := range resp {
		if len(r.Errors) > 0 {
			t.Errorf("response has errors: %v", r.Errors)
		}
	}
}

func TestNormalizeModelKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"GPT-4o mini", "gpt4omini"},
		{"GPT-4o", "gpt4o"},
		{"gpt-4o-mini-2024-07-18", "gpt4omini"},
		{"gpt-4o-2024-08-06", "gpt4o"},
		{"text-embedding-ada-002-v2", "textembeddingada002v2"},
		{"dall-e-3", "dalle3"},
		{"Image models", "imagemodels"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeModelKey(tt.input)
		if got != tt.want {
			t.Errorf("normalizeModelKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsDateLike(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"2024-07-18", true},
		{"2024-08-06", true},
		{"not-a-date", false},
		{"20240718", false},
		{"2024-7-18", false},
	}
	for _, tt := range tests {
		got := isDateLike(tt.input)
		if got != tt.want {
			t.Errorf("isDateLike(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestBuildTokenMap(t *testing.T) {
	model := "gpt-4o-2024-08-06"
	projectID := "proj_abc"

	usage := &openaiplugin.CompletionsUsageResponse{
		Data: []openaiplugin.CompletionsTimeBucket{
			{
				Results: []openaiplugin.CompletionsUsageResult{
					{
						InputTokens:  100,
						OutputTokens: 50,
						Model:        &model,
						ProjectID:    &projectID,
					},
					{
						InputTokens:  200,
						OutputTokens: 75,
						Model:        &model,
						ProjectID:    &projectID,
					},
				},
			},
		},
	}

	tokenMap := buildTokenMap(usage)

	key := normalizeModelKey(model) + "|" + projectID
	expected := 100 + 50 + 200 + 75
	if tokenMap[key] != expected {
		t.Errorf("buildTokenMap key %q = %d, want %d", key, tokenMap[key], expected)
	}
}

func TestBuildTokenMapNil(t *testing.T) {
	tokenMap := buildTokenMap(nil)
	if len(tokenMap) != 0 {
		t.Errorf("expected empty token map for nil input, got %d entries", len(tokenMap))
	}
}

func TestBuildCustomCosts(t *testing.T) {
	lineItem := "GPT-4o"
	projectID := "proj_abc"
	model := "gpt-4o-2024-08-06"

	costs := &openaiplugin.CostsResponse{
		Data: []openaiplugin.CostsTimeBucket{
			{
				Results: []openaiplugin.CostsResult{
					{
						Amount:    openaiplugin.CostAmount{Value: 0.42, Currency: "usd"},
						LineItem:  &lineItem,
						ProjectID: &projectID,
					},
				},
			},
		},
	}

	usage := &openaiplugin.CompletionsUsageResponse{
		Data: []openaiplugin.CompletionsTimeBucket{
			{
				Results: []openaiplugin.CompletionsUsageResult{
					{
						InputTokens:  1000,
						OutputTokens: 500,
						Model:        &model,
						ProjectID:    &projectID,
					},
				},
			},
		},
	}

	customCosts := buildCustomCosts(costs, usage)

	if len(customCosts) != 1 {
		t.Fatalf("expected 1 custom cost, got %d", len(customCosts))
	}

	cc := customCosts[0]
	if cc.BilledCost != float32(0.42) {
		t.Errorf("BilledCost = %f, want 0.42", cc.BilledCost)
	}
	if cc.ResourceName != "GPT-4o" {
		t.Errorf("ResourceName = %q, want %q", cc.ResourceName, "GPT-4o")
	}
	if cc.UsageQuantity != 1500 {
		t.Errorf("UsageQuantity = %f, want 1500", cc.UsageQuantity)
	}
	if cc.ChargeCategory != "Usage" {
		t.Errorf("ChargeCategory = %q, want %q", cc.ChargeCategory, "Usage")
	}
}

func TestBuildCustomCostsSkipsZero(t *testing.T) {
	lineItem := "GPT-4o"
	projectID := "proj_abc"

	costs := &openaiplugin.CostsResponse{
		Data: []openaiplugin.CostsTimeBucket{
			{
				Results: []openaiplugin.CostsResult{
					{
						Amount:    openaiplugin.CostAmount{Value: 0, Currency: "usd"},
						LineItem:  &lineItem,
						ProjectID: &projectID,
					},
				},
			},
		},
	}

	customCosts := buildCustomCosts(costs, nil)

	if len(customCosts) != 0 {
		t.Errorf("expected 0 custom costs for zero-value entry, got %d", len(customCosts))
	}
}

func TestBuildCustomCostsNilInputs(t *testing.T) {
	customCosts := buildCustomCosts(nil, nil)
	if customCosts != nil {
		t.Errorf("expected nil for nil costs input, got %d entries", len(customCosts))
	}
}

func TestBuildCustomCostsNoTokenMatch(t *testing.T) {
	lineItem := "Image models"
	projectID := "proj_abc"

	costs := &openaiplugin.CostsResponse{
		Data: []openaiplugin.CostsTimeBucket{
			{
				Results: []openaiplugin.CostsResult{
					{
						Amount:    openaiplugin.CostAmount{Value: 1.50, Currency: "usd"},
						LineItem:  &lineItem,
						ProjectID: &projectID,
					},
				},
			},
		},
	}

	customCosts := buildCustomCosts(costs, nil)

	if len(customCosts) != 1 {
		t.Fatalf("expected 1 custom cost, got %d", len(customCosts))
	}
	if customCosts[0].UsageQuantity != -1 {
		t.Errorf("UsageQuantity = %f, want -1 (no token match)", customCosts[0].UsageQuantity)
	}
}

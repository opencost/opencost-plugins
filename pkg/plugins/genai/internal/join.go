package internal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/opencost/opencost-plugins/pkg/plugins/genai/genaiprovider"
)

type NodeCapacityMap map[string]float64

type GenAIReport struct {
	Attributes GenAIAttributes   `json:"attributes"`
	Stats      EfficiencyMetrics `json:"stats"`
}

// based on  OpenTelemetry semantic conventions for GenAI.
type GenAIAttributes struct {
	WorkflowPhase string `json:"workflow_phase"`
	ModelName     string `json:"model_name"`
	ModelVersion  string `json:"model_version"`
	TenantID      string `json:"tenant_id"`
	Accelerator   string `json:"accelerator"`
	GPUUUID       string `json:"gpu_uuid"`
}

const (
	AnnotationWorkflowPhase = "opencost.io/workflow-phase"
	AnnotationModelName     = "opencost.io/gen-ai-model-name"
	AnnotationModelVersion  = "opencost.io/gen-ai-model-version"
	AnnotationTenantID      = "opencost.io/tenant-id"
	AnnotationAccelerator   = "opencost.io/accelerator-type"
	AnnotationGPUUUID       = "opencost.io/gpu-uuid"
)

func JoinPodToNodeMetrics(m *Metrics, nodeCap NodeCapacityMap, podCost float64, attr GenAIAttributes) GenAIReport {

	if m == nil {
		return GenAIReport{
			Attributes: attr,
			Stats:      EfficiencyMetrics{},
		}
	}

	profile, requested := FindMIGProfile(m.PodRequests)

	var infra *MIGEfficiency
	if profile != "" {
		if capacity, ok := nodeCap[profile]; ok && capacity > 0 {
			infra = &MIGEfficiency{
				MIGUtilization: (requested / capacity) * 100,
			}
		}
	}

	stats := CalculateEfficiency(m, infra, nodeCap, podCost)

	return GenAIReport{
		Attributes: attr,
		Stats:      stats,
	}
}

// ExtractGenAIAttributes pulls key metadata from Pod annotations.
func ExtractGenAIAttributes(annotations map[string]string) GenAIAttributes {
	return GenAIAttributes{
		WorkflowPhase: annotations[AnnotationWorkflowPhase],
		ModelName:     annotations[AnnotationModelName],
		ModelVersion:  annotations[AnnotationModelVersion],
		TenantID:      annotations[AnnotationTenantID],
		Accelerator:   annotations[AnnotationAccelerator],
		GPUUUID:       annotations[AnnotationGPUUUID],
	}
}

func GetNodeMIGCapacity(capacity map[string]float64) NodeCapacityMap {
	migCap := make(NodeCapacityMap)
	for resource, quantity := range capacity {
		// Only grab nvidia.com/mig-* resources
		if strings.HasPrefix(resource, "nvidia.com/mig-") {
			migCap[resource] = quantity
		}
	}
	return migCap
}

// GenAIWorkload represents a processed GenAI workload with cost information
type GenAIWorkload struct {
	PodName       string
	ModelName     string
	TotalTokens   int64
	TotalCost     float64
	TenantID      string
	WorkflowPhase string
	MIGProfile    string
	GPUEfficiency float64
	Efficiency    EfficiencyMetrics
	Attributes    GenAIAttributes
}

// CalculateGenAIWorkloads fetches metrics and calculates costs for GenAI workloads
func CalculateGenAIWorkloads(start, end time.Time, config *genaiprovider.Config) ([]GenAIWorkload, error) {
	if config.PrometheusURL == "" {
		return nil, fmt.Errorf("prometheus URL is not configured")
	}

	querier, err := NewHttpPrometheusQuerier(config.PrometheusURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus client: %w", err)
	}

	provider := NewPrometheusProvider(querier)
	metricsMap, err := provider.Fetch(context.Background(), start, end, DefaultMetricMapping(), config.ClusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metrics: %w", err)
	}

	// Fetch actual pod costs from OpenCost
	openCostURL := config.OpenCostURL
	if openCostURL == "" {
		openCostURL = "http://localhost:9003"
	}
	podCosts, err := FetchPodCosts(openCostURL, start, end)
	if err != nil {
		// Log error but gracefully continue? OpenCost might be down. Better to fail fast since it's the core prop.
		return nil, fmt.Errorf("failed to fetch pod costs from OpenCost: %w", err)
	}

	podRequests, err := provider.FetchMIGRequests(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch mig requests: %w", err)
	}

	nodeCap, err := provider.FetchNodeMIGCapacity(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch node mig capacity: %w", err)
	}

	var workloads []GenAIWorkload
	for _, m := range metricsMap {
		// Try to lookup cost by namespace/pod. Fallbacks for standard OpenCost ID.
		podKey := fmt.Sprintf("%s/%s", m.Namespace, m.Pod)
		podCost := podCosts[podKey]
		if podCost <= 0 {
			// Sometime OpenCost formats it differently like cluster/namespace/pod
			podCost = podCosts[fmt.Sprintf("%s/%s/%s", m.Cluster, m.Namespace, m.Pod)]
		}

		if reqs, ok := podRequests[podKey]; ok {
			m.PodRequests = reqs
		}

		attr := GenAIAttributes{
			WorkflowPhase: m.WorkflowPhase,
			ModelName:     m.ModelName,
			// Tenant, Version, etc. could also be mapped if available from annotations or metrics
		}

		report := JoinPodToNodeMetrics(m, nodeCap, podCost, attr)
		migProfile, _ := FindMIGProfile(m.PodRequests)

		workloads = append(workloads, GenAIWorkload{
			PodName:       m.Pod, // Formatted as namespace/pod in metricsMap, mapResults populated Pod specifically
			ModelName:     m.ModelName,
			TotalTokens:   int64(m.InputTokens + m.OutputTokens),
			TotalCost:     podCost,
			TenantID:      attr.TenantID,
			WorkflowPhase: m.WorkflowPhase,
			MIGProfile:    migProfile,
			GPUEfficiency: report.Stats.GPUUtilPercent,
			Efficiency:    report.Stats,
			Attributes:    report.Attributes,
		})
	}

	return workloads, nil
}

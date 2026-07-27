package genaiprovider

import (
	"context"
	"time"
)

// CustomCostSource is the interface that your GenAI plugin must satisfy.
// This is the "Brain" that your main.go will implement.
type CustomCostSource interface {
	GetCustomCosts(req *CustomCostRequest) ([]*GenAIWorkloadData, error)
}

// EfficiencyMetricsData mirrors the efficiency math outputs
type EfficiencyMetricsData struct {
	CostPer1MInput  float64
	CostPer1MOutput float64
	CostPer1MTotal  float64
	TokensPerGPUSec float64
	CacheSavings    float64
	GPUUtilPercent  float64
	GPUWaste        float64
	EfficiencyStatus string
	MIGUtilization  float64
	UnallocatedCost float64
}

// GenAIAttributesData mirrors the GenAI metadata
type GenAIAttributesData struct {
	WorkflowPhase string
	ModelName     string
	ModelVersion  string
	TenantID      string
	Accelerator   string
	GPUUUID       string
}

// GenAIWorkloadData represents a processed GenAI workload with cost information
type GenAIWorkloadData struct {
	PodName       string
	ModelName     string
	TotalTokens   int64
	TotalCost     float64
	TenantID      string
	WorkflowPhase string
	MIGProfile    string
	GPUEfficiency float64
	
	Efficiency *EfficiencyMetricsData
	Attributes *GenAIAttributesData
}

// CustomCostRequest represents the time window OpenCost is asking for.
type CustomCostRequest struct {
	Start      time.Time
	End        time.Time
	Resolution string // e.g., "hourly" or "daily"
}

// --- gRPC Implementation ---

// GRPCServer handles incoming requests from OpenCost and sends them to your Plugin.
type GRPCServer struct {
	// This is a standard OpenCost requirement to avoid proto-version mismatch.
	UnimplementedCustomCostSourceServer
	Impl CustomCostSource
}

func (m *GRPCServer) GetCustomCosts(ctx context.Context, req *GetCustomCostsRequest) (*GetCustomCostsResponse, error) {
	// Translate the gRPC request into your Go struct
	r := &CustomCostRequest{
		Start: time.Unix(req.Start, 0),
		End:   time.Unix(req.End, 0),
	}

	// Call your math logic
	results, err := m.Impl.GetCustomCosts(r)
	if err != nil {
		return nil, err
	}

	// Translate results back to gRPC response
	var workloads []*GenAIWorkload
	for _, res := range results {
		wl := &GenAIWorkload{
			PodName:       res.PodName,
			ModelName:     res.ModelName,
			TotalTokens:   res.TotalTokens,
			TotalCost:     res.TotalCost,
			TenantId:      res.TenantID,
			WorkflowPhase: res.WorkflowPhase,
			MigProfile:    res.MIGProfile,
			GpuEfficiency: res.GPUEfficiency,
		}

		if res.Efficiency != nil {
			wl.Efficiency = &EfficiencyMetrics{
				CostPer_1MInput:  res.Efficiency.CostPer1MInput,
				CostPer_1MOutput: res.Efficiency.CostPer1MOutput,
				CostPer_1MTotal:  res.Efficiency.CostPer1MTotal,
				TokensPerGpuSec:  res.Efficiency.TokensPerGPUSec,
				CacheSavings:     res.Efficiency.CacheSavings,
				GpuUtilPercent:   res.Efficiency.GPUUtilPercent,
				GpuWaste:         res.Efficiency.GPUWaste,
				EfficiencyStatus: res.Efficiency.EfficiencyStatus,
				MigUtilization:   res.Efficiency.MIGUtilization,
				UnallocatedCost:  res.Efficiency.UnallocatedCost,
			}
		}

		if res.Attributes != nil {
			wl.Attributes = &GenAIAttributes{
				WorkflowPhase: res.Attributes.WorkflowPhase,
				ModelName:     res.Attributes.ModelName,
				ModelVersion:  res.Attributes.ModelVersion,
				TenantId:      res.Attributes.TenantID,
				Accelerator:   res.Attributes.Accelerator,
				GpuUuid:       res.Attributes.GPUUUID,
			}
		}

		workloads = append(workloads, wl)
	}

	return &GetCustomCostsResponse{Workloads: workloads}, nil
}

// GRPCClient is what the OpenCost Core uses to talk to your Plugin.
type GRPCClient struct {
	client CustomCostSourceClient
}

func (m *GRPCClient) GetCustomCosts(req *CustomCostRequest) ([]*GenAIWorkloadData, error) {
	// Translate Go struct to gRPC
	resp, err := m.client.GetCustomCosts(context.Background(), &GetCustomCostsRequest{
		Start: req.Start.Unix(),
		End:   req.End.Unix(),
	})
	if err != nil {
		return nil, err
	}

	// Translate gRPC response back to Go slice
	var results []*GenAIWorkloadData
	for _, w := range resp.Workloads {
		wl := &GenAIWorkloadData{
			PodName:       w.PodName,
			ModelName:     w.ModelName,
			TotalTokens:   w.TotalTokens,
			TotalCost:     w.TotalCost,
			TenantID:      w.TenantId,
			WorkflowPhase: w.WorkflowPhase,
			MIGProfile:    w.MigProfile,
			GPUEfficiency: w.GpuEfficiency,
		}

		if w.Efficiency != nil {
			wl.Efficiency = &EfficiencyMetricsData{
				CostPer1MInput:  w.Efficiency.CostPer_1MInput,
				CostPer1MOutput: w.Efficiency.CostPer_1MOutput,
				CostPer1MTotal:  w.Efficiency.CostPer_1MTotal,
				TokensPerGPUSec: w.Efficiency.TokensPerGpuSec,
				CacheSavings:    w.Efficiency.CacheSavings,
				GPUUtilPercent:  w.Efficiency.GpuUtilPercent,
				GPUWaste:        w.Efficiency.GpuWaste,
				EfficiencyStatus: w.Efficiency.EfficiencyStatus,
				MIGUtilization:  w.Efficiency.MigUtilization,
				UnallocatedCost: w.Efficiency.UnallocatedCost,
			}
		}

		if w.Attributes != nil {
			wl.Attributes = &GenAIAttributesData{
				WorkflowPhase: w.Attributes.WorkflowPhase,
				ModelName:     w.Attributes.ModelName,
				ModelVersion:  w.Attributes.ModelVersion,
				TenantID:      w.Attributes.TenantId,
				Accelerator:   w.Attributes.Accelerator,
				GPUUUID:       w.Attributes.GpuUuid,
			}
		}

		results = append(results, wl)
	}
	return results, nil
}

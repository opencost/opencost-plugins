package internal

import (
	"strings"
)

type EfficiencyMetrics struct {
	CostPer1MInput  float64 `json:"costPer1MInput"`
	CostPer1MOutput float64 `json:"costPer1MOutput"`
	CostPer1MTotal  float64 `json:"costPer1MTotal"`
	TokensPerGPUSec float64 `json:"tokensPerGPUSec"`
	CacheSavings    float64 `json:"cacheSavings"`

	GPUUtilPercent   float64 `json:"gpuUtilPercent"`
	GPUWaste         float64 `json:"gpuWaste"`
	EfficiencyStatus string  `json:"efficiencyStatus"`

	MIGUtilization  float64 `json:"migUtilization"`
	UnallocatedCost float64 `json:"unallocatedCost"`
}

func FindMIGProfile(podRequests map[string]float64) (string, float64) {
	for resourceName, quantity := range podRequests {
		if strings.HasPrefix(resourceName, "nvidia.com/mig-") {
			return resourceName, quantity
		}
	}
	return "", 0
}

func GetTokensPerGPUSecond(totalTokens, normalizedGPUSec float64) float64 {
	if normalizedGPUSec <= 0 {
		return 0
	}
	return totalTokens / normalizedGPUSec
}

func GetCostPerMillionTokens(tokens, cost float64) float64 {
	if tokens <= 0 {
		return 0
	}
	return (cost / tokens) * 1_000_000
}

func GetWeightedTokenCost(tokenCount, totalTokens, totalCost float64) float64 {
	if totalTokens <= 0 {
		return 0
	}
	return (tokenCount / totalTokens) * totalCost
}

func GetGPUWaste(podCost, gpuUtilPercent float64) float64 {
	if podCost <= 0 {
		return 0
	}

	util := gpuUtilPercent
	if util < 0 {
		util = 0
	}
	if util > 100 {
		util = 100
	}
	return podCost * (1.0 - (util / 100.0))
}

func GetGPUEfficiencyStatus(gpuUtilPercent float64) string {
	if gpuUtilPercent < 15 {
		return "Underutilized"
	} else if gpuUtilPercent > 85 {
		return "Saturated"
	}
	return "Optimal"
}

func CalculateEfficiency(m *Metrics, infra *MIGEfficiency, nodeCapacityMap map[string]float64, podCost float64) EfficiencyMetrics {
	if m == nil || podCost <= 0 {
		return EfficiencyMetrics{}
	}
	m.Sanitize()

	normalizedSec := m.GPUActiveSec
	profile, requested := FindMIGProfile(m.PodRequests)

	if profile != "" && nodeCapacityMap != nil {
		if capacity, ok := nodeCapacityMap[profile]; ok && capacity > 0 {
			normalizedSec = m.GPUActiveSec * (requested / capacity)
		}
	}

	totalTokens := m.InputTokens + m.OutputTokens

	res := EfficiencyMetrics{
		GPUUtilPercent:   m.GPUUtilPercent,
		GPUWaste:         GetGPUWaste(podCost, m.GPUUtilPercent),
		EfficiencyStatus: GetGPUEfficiencyStatus(m.GPUUtilPercent),
	}

	if infra != nil {
		res.MIGUtilization = infra.MIGUtilization
	}

	if totalTokens > 0 {
		inputShare := GetWeightedTokenCost(m.InputTokens, totalTokens, podCost)
		outputShare := GetWeightedTokenCost(m.OutputTokens, totalTokens, podCost)

		res.TokensPerGPUSec = GetTokensPerGPUSecond(totalTokens, normalizedSec)
		res.CostPer1MTotal = GetCostPerMillionTokens(totalTokens, podCost)
		res.CostPer1MInput = GetCostPerMillionTokens(m.InputTokens, inputShare)
		res.CostPer1MOutput = GetCostPerMillionTokens(m.OutputTokens, outputShare)
	}

	return res
}

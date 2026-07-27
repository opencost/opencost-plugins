package internal

// MIGEfficiency represents the infrastructure-level waste for a GPU node
type MIGEfficiency struct {
	RequestedSlices float64 `json:"requestedSlices"`
	TotalCapacity   float64 `json:"totalCapacity"`
	MIGUtilization  float64 `json:"migUtilization"` // % of slices used
}

type NodeMIGAggregation struct {
	NodeName string

	MIGUtilizationPercent float64
	UnallocatedGPUCostUSD float64
}

// CalculateMIGEfficiency determines how much of the physical GPU is "dark".
func CalculateMIGEfficiency(requested, capacity, totalNodeGPUCost float64) MIGEfficiency {
	if capacity <= 0 {
		return MIGEfficiency{}
	}

	// Percentage of the card assigned to pods
	utilization := (requested / capacity) * 100.0

	return MIGEfficiency{
		RequestedSlices: requested,
		TotalCapacity:   capacity,
		MIGUtilization:  utilization,
	}
}

// CalculateNodeMIGAggregation computes node-level GPU waste using total node GPU cost.
// This must NOT be called per pod.
func CalculateNodeMIGAggregation(nodeName string, requestedSlices float64, totalSlices float64, totalNodeGPUCost float64) NodeMIGAggregation {

	if totalSlices <= 0 || totalNodeGPUCost <= 0 {
		return NodeMIGAggregation{
			NodeName: nodeName,
		}
	}

	utilFraction := requestedSlices / totalSlices
	unallocatedCost := totalNodeGPUCost * (1.0 - utilFraction)

	return NodeMIGAggregation{
		NodeName:              nodeName,
		MIGUtilizationPercent: utilFraction * 100.0,
		UnallocatedGPUCostUSD: unallocatedCost,
	}
}

package request

import (
	"time"

	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
)

func ValidateRequest(req *pb.CustomCostRequest) []string {
	var errors []string
	now := time.Now()
	// 1. Check if resolution is less than a day
	if req.Resolution.AsDuration() < 24*time.Hour {
		var resolutionMessage = "Resolution should be at least one day."
		log.Warnf(resolutionMessage)
		errors = append(errors, resolutionMessage)
	}
	// Get the start of the current month
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	// 2. Check if start time is before the start of the current month
	if req.Start.AsTime().Before(currentMonthStart) {
		var startDateMessage = "Start date cannot be before the current month. Historical costs not currently supported"
		log.Warnf(startDateMessage)
		errors = append(errors, startDateMessage)
	}

	// 3. Check if end time is before the start of the current month
	if req.End.AsTime().Before(currentMonthStart) {
		var endDateMessage = "End date cannot be before the current month. Historical costs not currently supported"
		log.Warnf(endDateMessage)
		errors = append(errors, endDateMessage)
	}

	return errors
}

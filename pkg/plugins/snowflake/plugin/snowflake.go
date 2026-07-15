package plugin

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/opencost/opencost-plugins/pkg/plugins/snowflake/config"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/util/uuid"
)

type SnowflakeCostSource struct {
	db *sql.DB
}

func NewSnowflakeCostSource(config *config.SnowflakeConfig) (*SnowflakeCostSource, error) {
	// Create connection string with all parameters
	dsn := fmt.Sprintf("%s:%s@%s/%s/%s?warehouse=%s&role=%s",
		config.Username,
		config.Password,
		config.Account,
		config.Database,
		"ACCOUNT_USAGE",
		config.Warehouse,
		config.Role,
	)

	db, err := sql.Open("snowflake", dsn)
	if err != nil {
		return nil, fmt.Errorf("error connecting to Snowflake: %v", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("error connecting to Snowflake: %v", err)
	}

	_, err = db.Exec("ALTER SESSION SET TIMEZONE = UTC")
	if err != nil {
		return nil, fmt.Errorf("error setting timezone: %v", err)
	}

	_, err = db.Exec("SELECT 1 FROM WAREHOUSE_METERING_HISTORY LIMIT 1")
	if err != nil {
		return nil, fmt.Errorf("error accessing ACCOUNT_USAGE schema: %v", err)
	}

	return &SnowflakeCostSource{
		db: db,
	}, nil
}

func (s *SnowflakeCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
	results := []*pb.CustomCostResponse{}

	windows, err := opencost.GetWindows(req.Start.AsTime(), req.End.AsTime(), req.Resolution.AsDuration())
	if err != nil {
		errResp := pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting windows: %v", err)},
		}
		results = append(results, &errResp)
		return results
	}

	for _, window := range windows {
		if window.Start().After(time.Now().UTC()) {
			continue
		}

		result := s.getCostsForWindow(&window)
		results = append(results, result)
	}

	return results
}

func (s *SnowflakeCostSource) getCostsForWindow(window *opencost.Window) *pb.CustomCostResponse {
	var costs []*pb.CustomCost

	warehouseCosts, err := s.getWarehouseCosts(window)
	if err != nil {
		return &pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting warehouse costs: %v", err)},
		}
	}
	costs = append(costs, warehouseCosts...)

	storageCosts, err := s.getStorageCosts(window)
	if err != nil {
		return &pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting storage costs: %v", err)},
		}
	}
	costs = append(costs, storageCosts...)

	transferCosts, err := s.getDataTransferCosts(window)
	if err != nil {
		return &pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting data transfer costs: %v", err)},
		}
	}
	costs = append(costs, transferCosts...)

	return &pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v1"},
		CostSource: "data_warehouse",
		Domain:     "snowflake",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*window.Start()),
		End:        timestamppb.New(*window.End()),
		Errors:     []string{},
		Costs:      costs,
	}
}

func (s *SnowflakeCostSource) getWarehouseCosts(window *opencost.Window) ([]*pb.CustomCost, error) {
	var costs []*pb.CustomCost

	query := `
		SELECT 
			WAREHOUSE_NAME,
			SUM(CREDITS_USED_COMPUTE) as compute_credits,
			SUM(CREDITS_USED_CLOUD_SERVICES) as cloud_services_credits
		FROM WAREHOUSE_METERING_HISTORY
		WHERE START_TIME >= ?
		AND END_TIME <= ?
		GROUP BY WAREHOUSE_NAME
	`

	rows, err := s.db.Query(query, window.Start(), window.End())
	if err != nil {
		return nil, fmt.Errorf("error querying warehouse metering: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var warehouseName string
		var computeCredits, cloudServicesCredits float64

		err := rows.Scan(&warehouseName, &computeCredits, &cloudServicesCredits)
		if err != nil {
			continue
		}

		costs = append(costs, &pb.CustomCost{
			AccountName:    warehouseName,
			ChargeCategory: "Compute",
			Description:    fmt.Sprintf("Compute credits for warehouse %s", warehouseName),
			ResourceName:   warehouseName,
			Id:             string(uuid.NewUUID()),
			ProviderId:     fmt.Sprintf("%s/compute", warehouseName),
			BilledCost:     float32(computeCredits * 3.0),
			ListCost:       float32(computeCredits * 3.0),
			UsageQuantity:  float32(computeCredits),
			UsageUnit:      "credits",
		})

		costs = append(costs, &pb.CustomCost{
			AccountName:    warehouseName,
			ChargeCategory: "Cloud Services",
			Description:    fmt.Sprintf("Cloud services credits for warehouse %s", warehouseName),
			ResourceName:   warehouseName,
			Id:             string(uuid.NewUUID()),
			ProviderId:     fmt.Sprintf("%s/cloud-services", warehouseName),
			BilledCost:     float32(cloudServicesCredits * 3.0),
			ListCost:       float32(cloudServicesCredits * 3.0),
			UsageQuantity:  float32(cloudServicesCredits),
			UsageUnit:      "credits",
		})
	}

	return costs, nil
}

func (s *SnowflakeCostSource) getStorageCosts(window *opencost.Window) ([]*pb.CustomCost, error) {
	var costs []*pb.CustomCost

	query := `
		SELECT 
			USAGE_DATE,
			STORAGE_BYTES,
			STAGE_BYTES,
			FAILSAFE_BYTES
		FROM STORAGE_USAGE
		WHERE USAGE_DATE >= ?
		AND USAGE_DATE <= ?
	`

	rows, err := s.db.Query(query, window.Start(), window.End())
	if err != nil {
		return nil, fmt.Errorf("error querying storage usage: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var usageDate time.Time
		var storageBytes, stageBytes, failsafeBytes int64

		err := rows.Scan(&usageDate, &storageBytes, &stageBytes, &failsafeBytes)
		if err != nil {
			continue
		}

		totalBytes := storageBytes + stageBytes + failsafeBytes
		totalTB := float64(totalBytes) / (1024 * 1024 * 1024 * 1024) // Convert to TB

		costs = append(costs, &pb.CustomCost{
			AccountName:    "storage",
			ChargeCategory: "Storage",
			Description:    "Data storage usage",
			ResourceName:   "storage",
			Id:             string(uuid.NewUUID()),
			ProviderId:     "storage",
			BilledCost:     float32(totalTB * 23.0),
			ListCost:       float32(totalTB * 23.0),
			UsageQuantity:  float32(totalTB),
			UsageUnit:      "TB",
		})
	}

	return costs, nil
}

func (s *SnowflakeCostSource) getDataTransferCosts(window *opencost.Window) ([]*pb.CustomCost, error) {
	var costs []*pb.CustomCost

	query := `
		SELECT 
			TRANSFER_TYPE,
			SUM(BYTES_TRANSFERRED) as bytes_transferred
		FROM DATA_TRANSFER_HISTORY
		WHERE START_TIME >= ?
		AND END_TIME <= ?
		GROUP BY TRANSFER_TYPE
	`

	rows, err := s.db.Query(query, window.Start(), window.End())
	if err != nil {
		return nil, fmt.Errorf("error querying data transfer history: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var transferType string
		var bytesTransferred int64

		err := rows.Scan(&transferType, &bytesTransferred)
		if err != nil {
			continue
		}

		gbTransferred := float64(bytesTransferred) / (1024 * 1024 * 1024)

		costs = append(costs, &pb.CustomCost{
			AccountName:    "data-transfer",
			ChargeCategory: "Data Transfer",
			Description:    fmt.Sprintf("Data transfer - %s", transferType),
			ResourceName:   transferType,
			Id:             string(uuid.NewUUID()),
			ProviderId:     fmt.Sprintf("transfer/%s", transferType),
			BilledCost:     float32(gbTransferred * 0.02),
			ListCost:       float32(gbTransferred * 0.02),
			UsageQuantity:  float32(gbTransferred),
			UsageUnit:      "GB",
		})
	}

	return costs, nil
}

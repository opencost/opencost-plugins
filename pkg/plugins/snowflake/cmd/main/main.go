package main

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/hashicorp/go-plugin"
	commonconfig "github.com/opencost/opencost-plugins/common/config"
	commonrequest "github.com/opencost/opencost-plugins/common/request"
	snowflakeconfig "github.com/opencost/opencost-plugins/pkg/plugins/snowflake/config"
	snowflakeplugin "github.com/opencost/opencost-plugins/pkg/plugins/snowflake/plugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// handshakeConfigs are used to just do a basic handshake between
// a plugin and host. If the handshake fails, a user friendly error is shown.
// This prevents users from executing bad plugins or executing a plugin
// directory. It is a UX feature, not a security feature.
var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "snowflake",
}

// SnowflakeClient defines the interface for interacting with Snowflake
type SnowflakeClient interface {
	ExecuteQuery(query string) (*sql.Rows, error)
}

// snowflakeClient is the concrete implementation of the SnowflakeClient interface
type snowflakeClient struct {
	db *sql.DB
}

// NewSnowflakeClient creates and returns a new SnowflakeClient instance
// NewSnowflakeClient creates a new Snowflake client using the provided Snowflake configuration.
// It constructs a DSN (Data Source Name) string from the configuration and attempts to open a connection to Snowflake.
// If the connection is successful and the database is reachable, it returns a SnowflakeClient instance.
// If there is an error during the connection or ping process, it returns an error.
//
// Parameters:
//   - snowflakeConfig: A pointer to a SnowflakeConfig struct containing the necessary configuration details.
//
// Returns:
//   - SnowflakeClient: An instance of the SnowflakeClient interface if the connection is successful.
//   - error: An error if there is an issue with the connection or ping process.
func NewSnowflakeClient(snowflakeConfig *snowflakeconfig.SnowflakeConfig) (SnowflakeClient, error) {
	dsn := fmt.Sprintf("user=%s password=%s account=%s db=%s schema=%s warehouse=%s",
		snowflakeConfig.Username,
		snowflakeConfig.Password,
		snowflakeConfig.Account,
		snowflakeConfig.Database,
		snowflakeConfig.Schema,
		snowflakeConfig.Warehouse)

	// Open a connection to Snowflake
	db, err := sql.Open("snowflake", dsn)
	if err != nil {
		return nil, err
	}

	// Check if the connection is alive
	if err = db.Ping(); err != nil {
		return nil, err
	}

	return &snowflakeClient{db: db}, nil
}

// ExecuteQuery executes a SQL query and returns the resulting rows
func (s *snowflakeClient) ExecuteQuery(query string) (*sql.Rows, error) {
	return s.db.Query(query)
}

type SnowflakeCostSource struct {
	snowflakeClient SnowflakeClient
}

// GetInvoices fetches invoices from Snowflake
func GetInvoices(snowflakeClient SnowflakeClient) ([]snowflakeplugin.LineItem, error) {

	query := snowflakeplugin.CreditsByWarehouse()

	// Execute the query using the Snowflake client
	rows, err := snowflakeClient.ExecuteQuery(query)
	if err != nil {

		log.Fatalf("Query execution failed:", err)
		return nil, err
	}
	defer rows.Close()

	var lineItems []snowflakeplugin.LineItem

	for rows.Next() {
		var warehouse string
		var credits float32
		var date string

		if err := rows.Scan(&date, &warehouse, &credits); err != nil {
			log.Fatalf("", err)
		}

		lineItem := snowflakeplugin.LineItem{
			WarehouseName: warehouse,
			CreditUsed:    credits,
			Date:          date,
		}

		lineItems = append(lineItems, lineItem)
	}

	if err = rows.Err(); err != nil {
		log.Fatalf("", err)
	}

	return lineItems, nil

}

func main() {

	log.Debug("Initializing Snowflake plugin")

	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	snowflakeConfig, err := snowflakeconfig.GetSnowflakeConfig(configFile)
	if err != nil {
		log.Fatalf("error building Atlas config: %v", err)
	}
	log.SetLogLevel("info") //default
	if snowflakeConfig.LogLevel != "" {
		log.SetLogLevel(snowflakeConfig.LogLevel)

	}

	client, err := NewSnowflakeClient(snowflakeConfig)

	if err != nil {
		log.Fatalf("Failed to create Snowflake client:", err)
	}
	snowflakeCostSource := SnowflakeCostSource{
		snowflakeClient: client,
	}
	defer client.(*snowflakeClient).db.Close()

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: &snowflakeCostSource},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})

}
func (s *SnowflakeCostSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {

	results := []*pb.CustomCostResponse{}

	requestErrors := commonrequest.ValidateRequest(req)
	if len(requestErrors) > 0 {
		//return empty response
		return results
	}

	targets, err := opencost.GetWindows(req.Start.AsTime(), req.End.AsTime(), req.Resolution.AsDuration())
	if err != nil {
		log.Errorf("error getting windows: %v", err)
		errResp := pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error getting windows: %v", err)},
		}
		results = append(results, &errResp)
		return results
	}

	lineItems, err := GetInvoices(s.snowflakeClient)

	if err != nil {
		log.Errorf("Error fetching invoices: %v", err)
		errResp := pb.CustomCostResponse{
			Errors: []string{fmt.Sprintf("error fetching invoices: %v", err)},
		}
		results = append(results, &errResp)
		return results

	}
	//TODO convert target to CustomCostResponse
	for _, target := range targets {
		if target.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", target)
			continue
		}

		log.Debugf("fetching atlas costs for window %v", target)
		result := s.GetSnowflakeCostsForWindow(&target, lineItems)

		results = append(results, result)

	}

	return results

}
func filterLineItemsByWindow(win *opencost.Window, lineItems []snowflakeplugin.LineItem) []*pb.CustomCost {
	var filteredItems []*pb.CustomCost
	for _, li := range lineItems {
		if li.Date == win.Start().Format("2006-01-02 15:04:05") {
			cost := &pb.CustomCost{
				UsageQuantity: li.CreditUsed,
				ResourceName:  li.WarehouseName,
			}
			filteredItems = append(filteredItems, cost)
		}
	}
	return filteredItems
}

func (s *SnowflakeCostSource) GetSnowflakeCostsForWindow(win *opencost.Window, lineItems []snowflakeplugin.LineItem) *pb.CustomCostResponse {

	costsInWindow := filterLineItemsByWindow(win, lineItems)
	resp := pb.CustomCostResponse{
		Metadata:   map[string]string{"api_client_version": "v1"},
		CostSource: "data_storage",
		Domain:     "snowflake",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*win.Start()),
		End:        timestamppb.New(*win.End()),
		Errors:     []string{},
		Costs:      costsInWindow,
	}
	return &resp
}

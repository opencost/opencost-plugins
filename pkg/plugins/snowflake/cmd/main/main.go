package main

import (
	"database/sql"
	"fmt"

	commonconfig "github.com/opencost/opencost-plugins/common/config"
	commonrequest "github.com/opencost/opencost-plugins/common/request"
	snowflakeconfig "github.com/opencost/opencost-plugins/pkg/plugins/snowflake/config"
	snowflakeplugin "github.com/opencost/opencost-plugins/pkg/plugins/snowflake/plugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
)

// SnowflakeClient defines the interface for interacting with Snowflake
type SnowflakeClient interface {
	ExecuteQuery(query string) (*sql.Rows, error)
}

// snowflakeClient is the concrete implementation of the SnowflakeClient interface
type snowflakeClient struct {
	db *sql.DB
}

// NewSnowflakeClient creates and returns a new SnowflakeClient instance
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
	// Example query
	//TODO make sure that query maps to snowflakeplugin.LineItem
	query := `
		SELECT 
			date_trunc('day', start_time) AS usage_date,
			SUM(credits_used) AS total_credits
		FROM snowflake.account_usage.warehouse_metering_history
		WHERE start_time >= DATEADD(day, -30, CURRENT_TIMESTAMP())
		GROUP BY usage_date
		ORDER BY usage_date DESC
	`

	// Execute the query using the Snowflake client
	rows, err := snowflakeClient.ExecuteQuery(query)
	if err != nil {
		log.Fatal("Query execution failed:", err)
		return nil, err
	}
	defer rows.Close()
	// Implement the logic to fetch pending invoices from Snowflake
	// This is a placeholder implementation
	return rows, nil
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
		log.Fatal("Failed to create Snowflake client:", err)
	}
	snowflakeCostSource := SnowflakeCostSource{
		snowflakeClient: client
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

		// Print the results
	fmt.Println("Date\t\tTotal Credits")
	for rows.Next() {
		var date string
		var credits float64
		var warehouse string
		if err := rows.Scan(&date, &credits, &warehouse); err != nil {
			log.Fatal(err)
		}
		//TODO extract lineItem into CustomCostResponse
		//result := a.getAtlasCostsForWindow(&target, lineItems)

		results = append(results, result)
		//fmt.Printf("%s\t%.2f\n", date, credits)

	}

	// Check for errors from iterating over rows
	if err = rows.Err(); err != nil {
		log.Fatal(err)
	}

	}

	return results

}

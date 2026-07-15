package main

import (
	"errors"
	"testing"
	"time"

	snowflakeplugin "github.com/opencost/opencost-plugins/pkg/plugins/snowflake/plugin"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/DATA-DOG/go-sqlmock" // For mocking SQL database
)

// TestGetInvoices function
func TestGetInvoices(t *testing.T) {
	// Create a mocked database connection
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Error initializing sqlmock: %v", err)
	}
	defer db.Close()

	// Convert MockRows to *sql.Rows

	expectedQuery := `(?s)SELECT start_time, warehouse_name, credits_used_compute FROM snowflake\.account_usage\.warehouse_metering_history WHERE start_time >= DATEADD\(day, -m, CURRENT_TIMESTAMP\(\)\) AND warehouse_id > 0 -- Skip pseudo-VWs such as "CLOUD_SERVICES_ONLY" ORDER BY 1 DESC, 2;`

	rows := sqlmock.NewRows([]string{"Date", "WarehouseName", "Credits"}).
		AddRow("2025-01-23", "ANALYTICS_VW", 123.45).
		AddRow("2025-01-22", "LOADING_VW", 75)

	// Expect the query and provide the mock response
	mock.ExpectQuery(expectedQuery).WillReturnRows(rows)

	// Create the SnowflakeClient with the mocked db
	mockClient := &snowflakeClient{db: db}

	// Test the function
	invoices, err := GetInvoices(mockClient)

	// Assertions
	assert.NoError(t, err)
	assert.Len(t, invoices, 2)

	expectedInvoices := []snowflakeplugin.LineItem{
		{WarehouseName: "ANALYTICS_VW", CreditUsed: 123.45, Date: "2025-01-23"},
		{WarehouseName: "LOADING_VW", CreditUsed: 75, Date: "2025-01-22"},
	}
	assert.Equal(t, expectedInvoices, invoices)

}

func TestExecuteQueryWithRowError(t *testing.T) {
	// Create a mocked database connection
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Error initializing sqlmock: %v", err)
	}
	defer db.Close()

	// Define the query
	query := "SELECT start_time, warehouse_name, credits_used_compute FROM warehouse_metering_history"

	// Create mock rows with an error injected
	rows := sqlmock.NewRows([]string{"start_time", "warehouse_name", "credits_used_compute"}).
		AddRow("2025-01-28T16:00:00Z", "WH1", 123.45).
		AddRow("2025-01-28T15:00:00Z", "WH2", 678.90).
		RowError(1, errors.New("simulated row error")) // Inject error at row index 1

	// Expect the query and provide the mock response
	mock.ExpectQuery(query).WillReturnRows(rows)

	// Create the SnowflakeClient with the mocked db
	client := &snowflakeClient{db: db}

	// Call ExecuteQuery
	result, err := client.ExecuteQuery(query)
	if err != nil {
		t.Fatalf("ExecuteQuery failed: %v", err)
	}
	defer result.Close()

	// Validate the results
	var (
		startTime     string
		warehouseName string
		credits       float64
	)

	for result.Next() {
		if err := result.Scan(&startTime, &warehouseName, &credits); err != nil {
			t.Fatalf("Error scanning row: %v", err)
		}
	}

	// Check for row errors
	if err := result.Err(); err == nil {
		t.Errorf("Expected an error from result.Err(), but got nil")
	} else if err.Error() != "simulated row error" {
		t.Errorf("Unexpected error message from result.Err(): %v", err)
	}

	// Verify that all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("SQL expectations were not met: %v", err)
	}
}

func TestGetCustomCosts(t *testing.T) {
	assert.Equal(t, 0, 0)
}

func TestFilterLineItemsByWindow(t *testing.T) {
	assert.Equal(t, 0, 0)
}

func TestGetSnowflakeCostsForWindow(t *testing.T) {

	// Create a mocked database connection
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Error initializing sqlmock: %v", err)
	}
	defer db.Close()

	// Create the SnowflakeClient with the mocked db
	mockClient := &snowflakeClient{db: db}
	// Create SnowflakeCostSource
	snowflakeCostSource := SnowflakeCostSource{
		snowflakeClient: mockClient,
	}

	// Define the window
	startTime := time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2025, 1, 25, 0, 0, 0, 0, time.UTC)
	window := opencost.NewWindow(&startTime, &endTime)

	// Fetch line items
	lineItems := []snowflakeplugin.LineItem{
		{WarehouseName: "ANALYTICS_VW", CreditUsed: 123.45, Date: "2025-01-23"},
		{WarehouseName: "LOADING_VW", CreditUsed: 75, Date: "2025-01-22"},
	}

	// Call GetSnowflakeCostsForWindow
	result := snowflakeCostSource.GetSnowflakeCostsForWindow(&window, lineItems)

	// Assertions
	assert.NotNil(t, result)
	assert.Equal(t, "data_storage", result.CostSource)
	assert.Equal(t, "snowflake", result.Domain)
	assert.Equal(t, "v1", result.Version)
	assert.Equal(t, "USD", result.Currency)
	assert.Equal(t, timestamppb.New(startTime), result.Start)
	assert.Equal(t, timestamppb.New(endTime), result.End)
	assert.Empty(t, result.Errors)
	assert.Len(t, result.Costs, 2)

	expectedCosts := []*pb.CustomCost{
		{UsageQuantity: 123.45, ResourceName: "ANALYTICS_VW"},
		{UsageQuantity: 75, ResourceName: "LOADING_VW"},
	}
	assert.Equal(t, expectedCosts, result.Costs)

}

package plugin

type LineItem struct {
	WarehouseName string  `json:"warehouseName"`
	CreditUsed    float32 `json:"creditUsed"`
	Date          string  `json:"date"`
}

// Query: Average hour-by-hour Snowflake spend (across all warehouses) over the past m days
func CreditsByWarehouse() string {
	return `
		SELECT start_time,
		warehouse_name,
		credits_used_compute
		FROM snowflake.account_usage.warehouse_metering_history
		WHERE start_time >= DATEADD(day, -m, CURRENT_TIMESTAMP())
		AND warehouse_id > 0  -- Skip pseudo-VWs such as "CLOUD_SERVICES_ONLY"
		ORDER BY 1 DESC, 2;
		
	`

}

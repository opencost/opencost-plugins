package main

import (
	"database/sql"
	"fmt"

	commonconfig "github.com/opencost/opencost-plugins/common/config"
	snowflakeconfig "github.com/opencost/opencost-plugins/pkg/plugins/snowflake/config"
	"github.com/opencost/opencost/core/pkg/log"
)

func main() {
	//TODO get this information from the config file
	// Snowflake connection details
	log.Debug("Initializing Snowflake plugin")

	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	snowflakeConfig, err := snowflakeconfig.GetSnowflakeConfig(configFile)
	if err != nil {
		log.Fatalf("error building Atlas config: %v", err)
	}
	log.SetLogLevel(snowflakeConfig.LogLevel)
	dsn := fmt.Sprintf("user=%s password=%s account=%s db=%s schema=%s warehouse=%s",
		snowflakeConfig.Username,  // Replace with your Snowflake username
		snowflakeConfig.Password,  // Replace with your Snowflake password
		snowflakeConfig.Account,   // Replace with your Snowflake account name
		snowflakeConfig.Database,  // Replace with your database name
		snowflakeConfig.Schema,    // Replace with your schema name
		snowflakeConfig.Warehouse) // Replace with your warehouse name

	// Open a connection to Snowflake
	db, err := sql.Open("snowflake", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Check if the connection is alive
	err = db.Ping()
	if err != nil {
		log.Fatal(err)
	}

	// Example query to fetch costs; adjust this query based on your needs and Snowflake's usage data
	query := `
    SELECT 
        date_trunc('day', start_time) AS usage_date,
        SUM(credits_used) AS total_credits
    FROM snowflake.account_usage.warehouse_metering_history
    WHERE start_time >= DATEADD(day, -30, CURRENT_TIMESTAMP())
    GROUP BY usage_date
    ORDER BY usage_date DESC
    `

	rows, err := db.Query(query)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	// Print the results
	fmt.Println("Date\t\tTotal Credits")
	for rows.Next() {
		var date string
		var credits float64
		if err := rows.Scan(&date, &credits); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s\t%.2f\n", date, credits)
	}

	// Check for errors from iterating over rows
	if err = rows.Err(); err != nil {
		log.Fatal(err)
	}
}

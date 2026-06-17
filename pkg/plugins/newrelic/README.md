# New Relic OpenCost Plugin

The New Relic plugin imports New Relic usage data into OpenCost Custom Costs.

It uses New Relic NerdGraph:

```text
POST https://api.newrelic.com/graphql
```

New Relic documents NerdGraph access with the `API-Key` header and the `actor.account(id: ...).nrql(query: ...) { results }` GraphQL shape. New Relic billing usage docs recommend NRQL queries over `NrConsumption`, `NrMTDConsumption`, and `NrDailyUsage` for usage and billing analysis.

## Configuration

Create a plugin config JSON file:

```json
{
  "new_relic_api_key": "<new-relic-user-key>",
  "account_id": 123456,
  "gigabytes_ingested_unit_price": 0,
  "core_ccu_unit_price": 0,
  "advanced_ccu_unit_price": 0,
  "synthetic_check_unit_price": 0,
  "log_level": "info"
}
```

Optional fields:

```json
{
  "nerdgraph_url": "https://api.newrelic.com/graphql",
  "account_name": "production",
  "currency": "USD",
  "metric_prices": {
    "gigabytes_ingested": 0,
    "core_ccu": 0,
    "advanced_ccu": 0,
    "synthetic_checks": 0
  }
}
```

For EU data center accounts, set:

```json
{
  "nerdgraph_url": "https://api.eu.newrelic.com/graphql"
}
```

New Relic contract pricing depends on the customer's plan. Configure the unit price fields or `metric_prices` to convert usage into cost. When prices are left at zero, the plugin still reports usage quantities with zero billed cost.

## Default Queries

The plugin requests one NerdGraph NRQL result set per daily OpenCost window. Default queries use documented New Relic usage events and alias the measured value as `usageQuantity`:

- `NrConsumption` `sum(GigabytesIngested)` where `productLine = 'DataPlatform'`, faceted by `usageMetric` and `consumingAccountId`
- `NrConsumption` `sum(consumption)` where `metric = 'CoreCCU'`, faceted by `dimension_productCapability` and `consumingAccountId`
- `NrConsumption` `sum(consumption)` where `metric = 'AdvancedCCU'`, faceted by `dimension_productCapability` and `consumingAccountId`
- `NrDailyUsage` synthetic check counts, faceted by `syntheticsTypeLabel`, `syntheticsMonitorName`, and `consumingAccountId`

Only daily resolution is supported.

## Custom Queries

You can replace the default query set with `usage_queries`:

```json
{
  "new_relic_api_key": "<new-relic-user-key>",
  "account_id": 123456,
  "usage_queries": [
    {
      "name": "full_platform_users",
      "nrql": "FROM NrMTDConsumption SELECT latest(FullPlatformUsersBillable) AS usageQuantity SINCE '{start}' UNTIL '{end}'",
      "quantity_attribute": "usageQuantity",
      "unit": "user",
      "unit_price": 0,
      "price_key": "full_platform_users",
      "resource_type": "FullPlatformUsers",
      "facet_attributes": []
    }
  ]
}
```

Supported NRQL template placeholders:

- `{start}` and `{end}` render as `YYYY-MM-DD HH:MM:SS UTC`
- `{start_rfc3339}` and `{end_rfc3339}` render as RFC3339
- `{start_unix}` and `{end_unix}` render as Unix seconds

For custom queries, alias the billed quantity as `usageQuantity` or set `quantity_attribute` to match the numeric result key returned by NerdGraph.

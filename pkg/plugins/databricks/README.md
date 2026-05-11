# Databricks OpenCost Plugin

The Databricks plugin imports Databricks account billable usage logs into OpenCost Custom Costs.

It uses the account-level billable usage download API:

```text
GET /api/2.0/accounts/{account_id}/usage/download
```

The API returns CSV usage logs for a requested month range. The plugin downloads each required month once per OpenCost request, filters the records into daily windows, and converts usage rows into Custom Costs.

## Configuration

Create a plugin config JSON file:

```json
{
  "account_id": "<databricks-account-id>",
  "token": "<databricks-account-token>",
  "sku_unit_prices": {
    "STANDARD_ALL_PURPOSE_COMPUTE": 0.55
  },
  "default_unit_price": 0,
  "log_level": "info"
}
```

Optional fields:

```json
{
  "account_api_base_url": "https://accounts.cloud.databricks.com",
  "include_personal_data": false
}
```

Databricks usage downloads provide usage quantity by SKU, while actual pricing varies by account contract. Configure `sku_unit_prices` for known per-unit prices and optionally set `default_unit_price` as a fallback. Rows without a configured price are reported with usage quantity and zero cost unless `default_unit_price` is set.

## Cost Mapping

- `usage_quantity` and `usage_unit` -> usage quantity and unit
- configured SKU unit price multiplied by `usage_quantity` -> billed and list cost
- `account_id` -> account name and account ID
- `workspace_id` -> sub-account ID
- `billing_origin_product` or `usage_type` -> service and resource type
- `sku_name` -> SKU ID
- `record_id` -> provider ID

Only daily resolution is supported. The plugin requests monthly CSV exports because Databricks exposes the account billable usage download API with `start_month` and `end_month` parameters.

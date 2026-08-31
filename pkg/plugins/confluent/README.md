# Confluent OpenCost Plugin

The Confluent plugin imports Confluent Cloud billing line items from the `billing/v1/costs` API into OpenCost Custom Costs.

## Configuration

Create a plugin config JSON file with a Confluent Cloud API key and secret:

```json
{
  "confluent_api_key": "<cloud-api-key>",
  "confluent_api_secret": "<cloud-api-secret>",
  "log_level": "info"
}
```

Optional fields:

```json
{
  "api_base_url": "https://api.confluent.cloud",
  "page_size": 5000
}
```

`page_size` defaults to `5000`, matching the Confluent costs API default, and may be set up to `10000`.

## Cost Mapping

The plugin queries `/billing/v1/costs` once per daily OpenCost window with `start_date` and `end_date`, follows `metadata.next` pagination links, and maps each Confluent billing line item into a Custom Cost:

- `amount` -> billed cost
- `original_amount` -> list cost
- `quantity` and `unit` -> usage quantity and unit
- `product` -> service and resource type
- `line_type` -> SKU ID
- resource environment ID -> account name and account ID
- resource ID/display name -> sub-account ID/name

Only daily resolution is supported because Confluent returns costs at daily granularity.

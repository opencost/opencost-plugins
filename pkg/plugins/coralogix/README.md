# CoraLogix OpenCost Plugin

The CoraLogix plugin imports CoraLogix Data Usage Service metrics into OpenCost Custom Costs.

It uses the CoraLogix management API:

```text
GET /mgmt/openapi/latest/dataplans/data-usage/v2
```

The API requires the `Authorization` header with a CoraLogix API key. CoraLogix documents the Data Usage Service response as `entries` with `dimensions`, `sizeGb`, `timestamp`, and `units`; the service overview also notes that data usage endpoints can return newline-delimited JSON streams and should be requested with `Accept: text/event-stream`.

## Configuration

Create a plugin config JSON file:

```json
{
  "coralogix_api_key": "<coralogix-api-key>",
  "size_gb_unit_price": 0,
  "unit_price": 0,
  "log_level": "info"
}
```

Optional fields:

```json
{
  "api_base_url": "https://api.coralogix.com/mgmt/openapi/latest",
  "date_range_style": "form",
  "resolution": "",
  "aggregate_by": [
    "AGGREGATE_BY_APPLICATION",
    "AGGREGATE_BY_SUBSYSTEM",
    "AGGREGATE_BY_PILLAR"
  ],
  "metric_prices": {
    "gb_sent": 0,
    "unit_consumption": 0
  },
  "currency": "USD"
}
```

CoraLogix actual pricing depends on the customer's plan. Configure `size_gb_unit_price`, `unit_price`, or `metric_prices` to convert usage into cost. When prices are left at zero, the plugin still reports usage quantities with zero billed cost.

`api_base_url` can be changed for other CoraLogix regions. Official v5 server examples include:

- `https://api.coralogix.com/mgmt/openapi/5`
- `https://api.eu2.coralogix.com/mgmt/openapi/5`
- `https://api.coralogix.us/mgmt/openapi/5`
- `https://api.cx498.coralogix.com/mgmt/openapi/5`
- `https://api.coralogix.in/mgmt/openapi/5`
- `https://api.coralogixsg.com/mgmt/openapi/5`
- `https://api.ap3.coralogix.com/mgmt/openapi/5`

`date_range_style` defaults to `form`, which follows OpenAPI query-object defaults by sending `fromDate` and `toDate`. If a CoraLogix deployment expects a different object encoding, supported values are `deep_object`, `dotted`, and `json`.

## Cost Mapping

- `sizeGb` -> a `gb_sent` Custom Cost row with usage unit `GB`
- `units` -> a `unit_consumption` Custom Cost row with usage unit `unit`
- configured metric price multiplied by usage quantity -> billed and list cost
- `application` and `subsystem` dimensions -> account and resource names
- `pillar`, `entity_type`, `dataset`, and `dataspace` dimensions -> resource classification
- CoraLogix dimensions -> Custom Cost descriptions and stable provider IDs

Only daily resolution is supported. The plugin requests one Data Usage Service range per daily OpenCost window.

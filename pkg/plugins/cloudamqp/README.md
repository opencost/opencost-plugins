# CloudAMQP OpenCost Plugin

The CloudAMQP plugin imports CloudAMQP Customer API invoice data into OpenCost Custom Costs.

It uses the Customer API invoice endpoint:

```text
GET /invoices/period?year=<year>&month=<month>
```

CloudAMQP invoices are monthly, so the plugin downloads each required monthly invoice once per OpenCost request and prorates invoice line totals across daily Custom Cost windows.

## Configuration

Create a plugin config JSON file:

```json
{
  "api_key": "<cloudamqp-customer-api-key>",
  "log_level": "info"
}
```

Optional fields:

```json
{
  "api_base_url": "https://customer.cloudamqp.com/api",
  "account_name": "cloudamqp",
  "currency": "USD"
}
```

The Customer API uses HTTP Basic Auth with an empty username and the API key as the password.

## Cost Mapping

- invoice line `amount`, `total`, `subtotal`, `price`, or `cost` -> monthly line amount
- monthly line amount divided by days in the month -> daily billed and list cost
- invoice `currency` -> response currency
- invoice customer name -> account name and account ID
- invoice line description/name/plan/product -> resource fields

If invoice line amounts are not present, the plugin falls back to prorating the invoice `total`.

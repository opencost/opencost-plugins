# Databricks OpenCost Plugin

The Databricks plugin imports Databricks account billable usage logs into OpenCost Custom Costs.

It uses the account-level billable usage download API:

```text
GET /api/2.0/accounts/{account_id}/usage/download
```

The API returns CSV usage logs for a requested month range. The plugin downloads each required month once per OpenCost request, filters the records into daily windows, and converts usage rows into Custom Costs.

## Configuration

Create a plugin config JSON file. Choose **one** of the authentication methods below.

### Authentication

The usage download API is an account-level API. It only accepts OAuth access tokens, which expire after one hour, so the plugin fetches and renews tokens itself.

**Option 1: service principal OAuth secret (OAuth M2M).**

```json
{
  "account_id": "<databricks-account-id>",
  "client_id": "<service-principal-application-id>",
  "client_secret": "<service-principal-oauth-secret>",
  "scope": "billing:read",
  "sku_unit_prices": {
    "STANDARD_ALL_PURPOSE_COMPUTE": 0.55
  },
  "default_unit_price": 0,
  "log_level": "info"
}
```

**Option 2: workload identity federation (no secret).** Recommended on Kubernetes. Mount a projected service account token into the OpenCost pod and create a [service principal federation policy](https://docs.databricks.com/dev-tools/auth/oauth-federation-policy/) that trusts your cluster's issuer, with subject `system:serviceaccount:<namespace>:<serviceaccount>` and a matching audience.

```json
{
  "account_id": "<databricks-account-id>",
  "client_id": "<service-principal-application-id>",
  "federated_token_file": "/var/run/secrets/databricks/token",
  "scope": "billing:read"
}
```

The file is read again each time the plugin exchanges it for a Databricks token, so in-place rotation (as Kubernetes does for projected tokens) is picked up. Omit `client_id` for an account-wide federation policy.

Example OpenCost Helm values for the projected token:

```yaml
extraVolumes:
  - name: databricks-token
    projected:
      sources:
        - serviceAccountToken:
            audience: databricks
            expirationSeconds: 3600
            path: token
opencost:
  exporter:
    extraVolumeMounts:
      - name: databricks-token
        mountPath: /var/run/secrets/databricks
        readOnly: true
```

**Option 3: static `token` (testing only).** A pre-issued access token. It stops working after about one hour.

`scope` defaults to `all-apis`. Set it to the scope assigned to the OAuth secret or federation policy. A secret limited to `billing:read` is refused when it asks for `all-apis`.

**Minimum permissions:** a service principal with the **Billing admin** account role and the `billing:read` scope can call the usage download API. Account admin is not required.

### Other options

```json
{
  "account_api_base_url": "https://accounts.cloud.databricks.com",
  "include_personal_data": false
}
```

Use `https://accounts.gcp.databricks.com` for Databricks on Google Cloud.

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

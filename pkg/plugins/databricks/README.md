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

- `dbus` / `usage_quantity` -> usage quantity, in unit `DBU` unless the CSV has a unit column
- configured SKU unit price -> list unit price; price multiplied by usage -> billed and list cost
- `account_id` -> account name and account ID
- `workspaceId` -> sub-account ID
- `billing_origin_product`, `usage_type` or `sku` -> service and resource type
- `sku` -> SKU ID
- `clusterCustomTags` -> cost metadata (for example `JobId`, `NotebookId`, `SqlEndpointId`, `ai.model_family`)
- resource name -> the most specific workload identifier available. For serverless usage `clusterId` is usually empty, so it comes from the tags:
  `model/<family>`, `job/<id>`, `notebook/<id>`, `sql-warehouse/<id>`, `pipeline/<clusterName>` for `dlt-execution-*`, then `clusterId`, then `serverless-<workload>/<workspace>` or `model-serving/<workspace>`, then the workspace ID
- `record_id` -> provider ID when present. The legacy CSV has none, and several rows can share workspace, SKU and hour (often with an empty `clusterId`), so the provider ID includes a stable hash of the row.

SKU names on GCP (and in some regions elsewhere) include a region suffix, for example `PREMIUM_SERVERLESS_SQL_COMPUTE_US_IOWA`. Keys in `sku_unit_prices` must match exactly. To get your list prices per SKU, query `system.billing.list_prices` (column `pricing.default`).

Serverless usage (SQL warehouses, notebooks, jobs, pipelines and model serving) is included in the usage download. Pipeline usage is billed under the jobs serverless SKU and is identified by its `dlt-execution-*` cluster name.

A month without usage (HTTP 204 from the API) is reported as no costs, not as an error.

Only daily resolution is supported. The plugin requests monthly CSV exports because Databricks exposes the account billable usage download API with `start_month` and `end_month` parameters.

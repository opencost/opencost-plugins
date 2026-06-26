# GitHub Billing Plugin

This plugin imports GitHub enhanced billing usage into OpenCost custom costs.
It supports organization and user billing usage endpoints and maps returned
line items such as Actions, Packages, and storage SKUs to FOCUS-style custom
costs.

## Configuration

```json
{
  "github_token": "github_pat_...",
  "account": "my-org",
  "account_type": "org",
  "log_level": "info"
}
```

`account_type` can be `org` or `user`. The token must have billing usage
permissions for the selected account.

Optional fields:

```json
{
  "api_base_url": "https://api.github.com",
  "api_version": "2022-11-28"
}
```

The plugin supports daily OpenCost query resolution because GitHub's billing
usage endpoint exposes year, month, and day query parameters.

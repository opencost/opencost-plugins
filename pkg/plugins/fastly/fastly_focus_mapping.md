# Fastly Plugin – FOCUS Attribute Mapping

This document provides comprehensive mapping between Fastly invoice data and the FinOps Open Cost and Usage Specification (FOCUS) attributes used in OpenCost custom costs.

## Table of Contents
- [Overview](#overview)
- [Core CustomCost Fields](#core-customcost-fields)
- [Extended Attributes (FOCUS)](#extended-attributes-focus)
- [Labels](#labels)
- [Charge Category Classification](#charge-category-classification)
- [Usage Unit Mapping](#usage-unit-mapping)
- [Complete Example](#complete-example)

---

## Overview

The Fastly plugin transforms Fastly billing invoice data into OpenCost's CustomCost format, which follows the FOCUS specification for cloud cost reporting. Each transaction line item from a Fastly invoice becomes a CustomCost record with FOCUS-compliant attributes.

**Key Principles:**
- **ProviderId**: Constructed as `{CustomerID}/{ProductName}/{UsageType}` for stable cross-month tracking
- **Id**: Unique UUID generated per cost item
- **Costs are prorated**: If an invoice period spans multiple OpenCost windows, costs are prorated by time overlap
- **Currency**: All costs reported in USD (Fastly's invoice currency)

---

## Core CustomCost Fields

| CustomCost Field | Fastly Source | Description | Required | Example |
|-----------------|---------------|-------------|----------|---------|
| **Id** | Generated (UUID) | Unique identifier for this cost item | Yes | `"550e8400-e29b-41d4-a716-446655440000"` |
| **ProviderId** | `{customer_id}/{product_name}/{usage_type}` | Stable resource identifier across billing periods | Yes | `"cust-abc123/CDN/bandwidth"` |
| **AccountName** | `customer_id` | Fastly customer/account ID | Yes | `"cust-abc123"` |
| **ChargeCategory** | Derived from `description` and `amount` | Type of charge (see [classification](#charge-category-classification)) | Yes | `"usage"`, `"credit"`, `"commitment"` |
| **Description** | `description` | Human-readable line item description | Yes | `"CDN Bandwidth - North America"` |
| **ResourceName** | `usage_type` | Type of usage being charged | Yes | `"bandwidth"`, `"requests"` |
| **ResourceType** | `product_group` | Broad service category | Yes | `"Full Site Delivery"`, `"Compute"` |
| **Zone** | `region` (default: "Global") | Geographic region | Yes | `"North America"`, `"Global"` |
| **BilledCost** | `amount` (prorated) | Actual cost charged for this window | Yes | `100.50` |
| **ListCost** | `amount` (prorated) | List price before discounts | Yes | `100.50` |
| **ListUnitPrice** | `rate` | Price per unit | Yes | `0.05` |
| **UsageQuantity** | `units` (prorated) | Amount of usage consumed | Yes | `2010.0` |
| **UsageUnit** | Derived from `usage_type` and `product_name` | Unit of measurement (see [mapping](#usage-unit-mapping)) | Yes | `"GB"`, `"requests"` |
| **Labels** | Multiple sources | Key-value metadata | No | See [Labels](#labels) |
| **ExtendedAttributes** | Multiple sources | FOCUS-compliant extended fields | No | See [Extended Attributes](#extended-attributes-focus) |

---

## Extended Attributes (FOCUS)

Extended attributes follow the [FOCUS specification](https://focus.finops.org/) for standardized cloud cost reporting.

### Billing Information

| FOCUS Attribute | Fastly Field | Type | Required | Description | Example |
|----------------|--------------|------|----------|-------------|---------|
| **BillingPeriodStart** | `billing_start_date` | Timestamp | No | Start of billing cycle (ISO 8601) | `"2024-01-01T00:00:00Z"` |
| **BillingPeriodEnd** | `billing_end_date` | Timestamp | No | End of billing cycle (ISO 8601) | `"2024-01-31T23:59:59Z"` |

### Account Hierarchy

| FOCUS Attribute | Fastly Field | Type | Required | Description | Example |
|----------------|--------------|------|----------|-------------|---------|
| **AccountId** | `customer_id` | String | Yes | Fastly customer/account ID | `"cust-abc123"` |
| **SubAccountId** | `product_line` | String | No | Functional grouping (e.g., "Network Services") | `"Delivery"` |

### Service Information

| FOCUS Attribute | Fastly Field | Type | Required | Description | Example |
|----------------|--------------|------|----------|-------------|---------|
| **ServiceCategory** | `product_group` | String | No | Broad service family | `"Full Site Delivery"`, `"Compute"` |
| **ServiceName** | `product_name` | String | No | Specific product name | `"CDN"`, `"Compute@Edge"` |

### Pricing Information

| FOCUS Attribute | Fastly Field | Type | Required | Description | Example |
|----------------|--------------|------|----------|-------------|---------|
| **PricingQuantity** | `units` | Float | No | Usage quantity (only if > 0) | `2010.0` |
| **PricingUnit** | `usage_type` | String | No | Unit of measure | `"bandwidth"`, `"requests"` |

### Discounts

| FOCUS Attribute | Fastly Field | Type | Required | Description | Example |
|----------------|--------------|------|----------|-------------|---------|
| **CommitmentDiscountId** | `credit_coupon_code` | String | No | Discount or credit coupon code | `"PROMO2024"` |

---

## Labels

Labels provide additional metadata not covered by FOCUS attributes.

| Label Key | Fastly Source | Description | Example |
|-----------|---------------|-------------|---------|
| `product_line` | `product_line` | Functional grouping | `"Network Services"` |
| `product_name` | `product_name` | Specific product | `"CDN"` |
| `credit_coupon_code` | `credit_coupon_code` | Discount code applied | `"PROMO2024"` |
| `currency` | `currency_code` | Invoice currency | `"USD"` |
| `cost_type` | Derived from `amount` | Charge vs credit | `"charge"` or `"credit"` |

**Label Values:**
- `cost_type`: Set to `"credit"` if `amount < 0`, otherwise `"charge"`

---

## Charge Category Classification

The `ChargeCategory` field is derived using the following rules (checked in order):

```go
func getChargeCategory(item TransactionLineItem) string {
    descLower := strings.ToLower(item.Description)

    switch {
    case strings.Contains(descLower, "minimum"):
        return "commitment"
    case strings.Contains(descLower, "credit") || item.Amount < 0:
        return "credit"
    case strings.Contains(descLower, "support"):
        return "support"
    case strings.Contains(descLower, "tax"):
        return "tax"
    default:
        return "usage"
    }
}
```

**Categories:**
- `"usage"`: Standard consumption charges (default)
- `"credit"`: Discounts, refunds, or negative amounts
- `"commitment"`: Minimum spend commitments
- `"support"`: Support plan charges
- `"tax"`: Tax charges

---

## Usage Unit Mapping

The `UsageUnit` is intelligently derived from `usage_type` and `product_name`:

### Primary Mapping (by usage_type)

| Usage Type Contains | UsageUnit | Examples |
|---------------------|-----------|----------|
| `"bandwidth"` | `"GB"` | Bandwidth transfer |
| `"request"` | `"requests"` | HTTP/API requests |
| `"compute"` | `"compute-hours"` | Compute execution time |
| `"storage"` | `"GB"` | Object storage |
| `"committed amount"` | `"USD"` | Financial commitments |
| `"minute"` | `"minutes"` | Time-based usage |
| `"hour"` | `"hours"` | Time-based usage |
| `"invocation"` | `"invocations"` | Function invocations |
| `"log"` | `"log-lines"` | Log entries |

### Fallback Mapping (by product_name)

| Product Name Contains | UsageUnit | Examples |
|-----------------------|-----------|----------|
| `"cdn"` | `"GB"` | CDN bandwidth |
| `"compute"` | `"compute-hours"` | Edge compute |
| `"waf"` or `"security"` | `"requests"` | Security requests |
| `"image"` | `"transformations"` | Image optimization |
| `"video"` | `"minutes"` | Video streaming |

**Default:** `"units"` if no match found

---

## Complete Example

### Input: Fastly Invoice Line Item

```json
{
  "customer_id": "cust-abc123",
  "invoice_id": "inv-2024-01",
  "billing_start_date": "2024-01-01T00:00:00Z",
  "billing_end_date": "2024-01-31T23:59:59Z",
  "currency_code": "USD",
  "transaction_line_items": [
    {
      "description": "CDN Bandwidth - North America",
      "amount": 150.75,
      "rate": 0.05,
      "units": 3015,
      "product_name": "CDN",
      "product_group": "Full Site Delivery",
      "product_line": "Network Services",
      "region": "North America",
      "usage_type": "bandwidth",
      "credit_coupon_code": ""
    }
  ]
}
```

### Output: OpenCost CustomCost

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "provider_id": "cust-abc123/CDN/bandwidth",
  "account_name": "cust-abc123",
  "charge_category": "usage",
  "description": "CDN Bandwidth - North America",
  "resource_name": "bandwidth",
  "resource_type": "Full Site Delivery",
  "zone": "North America",
  "billed_cost": 150.75,
  "list_cost": 150.75,
  "list_unit_price": 0.05,
  "usage_quantity": 3015.0,
  "usage_unit": "GB",
  "labels": {
    "product_line": "Network Services",
    "product_name": "CDN",
    "credit_coupon_code": "",
    "currency": "USD",
    "cost_type": "charge"
  },
  "extended_attributes": {
    "account_id": "cust-abc123",
    "sub_account_id": "Network Services",
    "billing_period_start": "2024-01-01T00:00:00Z",
    "billing_period_end": "2024-01-31T23:59:59Z",
    "service_category": "Full Site Delivery",
    "service_name": "CDN",
    "pricing_quantity": 3015.0,
    "pricing_unit": "bandwidth"
  }
}
```

---

## Implementation Notes

### Prorating
When an invoice period overlaps partially with the requested window, costs are prorated based on time:

```
prorateRatio = overlapHours / totalInvoiceHours
billedCost = amount * prorateRatio
usageQuantity = units * prorateRatio
```

### Region Handling
- If `region` is empty or null, defaults to `"Global"`
- Region values are passed through as-is from Fastly

### Optional Fields
- Fields marked as "No" in the Required column are only included if the source data is non-empty
- `PricingQuantity` is only included if `units > 0`
- Empty strings are treated as missing values

### Provider ID Stability
- Uses `CustomerID` (not `InvoiceID`) to ensure the same resource has a consistent identifier across months
- Format: `{CustomerID}/{ProductName}/{UsageType}`
- Example: `"cust-abc123/CDN/bandwidth"` stays the same in January and February invoices

---

## References

- [FOCUS Specification](https://focus.finops.org/)
- [Fastly Billing API Documentation](https://developer.fastly.com/reference/api/account/billing/)
- [OpenCost Plugin Development](https://github.com/opencost/opencost-plugins)
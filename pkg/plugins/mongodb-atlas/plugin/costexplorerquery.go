package plugin

type CreateCostExplorerQueryPayload struct {
	Clusters              []string `json:"clusters"`
	EndDate               string   `json:"endDate"`
	GroupBy               string   `json:"groupBy"`
	IncludePartialMatches bool     `json:"includePartialMatches"`
	Organizations         []string `json:"organizations"`
	Projects              []string `json:"projects"`
	Services              []string `json:"services"`
	StartDate             string   `json:"startDate"`
}

type CreateCostExplorerQueryResponse struct {
	Token string `json:"token"`
}

type Invoice struct {
	InvoiceId        string  `json:"invoiceId"`
	OrganizationId   string  `json:"organizationId"`
	OrganizationName string  `json:"organizationName"`
	Service          string  `json:"service"`
	UsageAmount      float32 `json:"usageAmount"`
	UsageDate        string  `json:"usageDate"`
	//"invoiceId":"66d7254246a21a41036ff315","organizationId":"66d7254246a21a41036ff2e9","organizationName":"Kubecost","service":"Clusters","usageAmount":51.19,"usageDate":"2024-09-01"}
}
type CostResponse struct {
	UsageDetails []Invoice `json:"usageDetails"`
}

type PendingInvoice struct {
	AmountBilledCents    int32      `json:"amountBilledCents"`
	AmountPaidCents      int32      `json:"amountPaidCents"`
	Created              string     `json:"created"`
	CreditsCents         int32      `json:"creditCents"`
	Id                   string     `json:"id"`
	EndDate              string     `json:"endDate"`
	LineItems            []LineItem `json:"lineItems"`
	Links                []Link     `json:"links"`
	OrgId                string     `json:"orgId"`
	SalesTaxCents        int32      `json:"salesTaxCents"`
	StartDate            string     `json:"startDate"`
	StartingBalanceCents int32      `json:"startingBalanceCents"`
	StatusName           string     `json:"statusName"`
	SubTotalCents        int32      `json:"subtotalCents"`
	Updated              string     `json:"updated"`
}

type Link struct {
	Href string `json:"href"`
	Rel  string `json:"rel"`
}

type LineItem struct {
	ClusterName      string  `json:"clusterName"`
	Created          string  `json:"created"`
	EndDate          string  `json:"endDate"`
	GroupId          string  `json:"groupId"`
	GroupName        string  `json:"groupName"`
	Quantity         float32 `json:"quantity"`
	SKU              string  `json:"sku"`
	StartDate        string  `json:"startDate"`
	TotalPriceCents  int32   `json:"totalPriceCents"`
	Unit             string  `json:"unit"`
	UnitPriceDollars float32 `json:"unitPriceDollars"`
}

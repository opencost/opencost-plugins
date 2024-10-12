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

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

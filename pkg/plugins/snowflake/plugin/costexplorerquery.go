package plugin

type LineItem struct {
	WarehouseName string  `json:"warehouseName"`
	CreditUsed    float32 `json:"creditUsed"`
	Date          string  `json:"date"`
}

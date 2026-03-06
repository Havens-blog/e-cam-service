package domain

// CostAnomaly 成本异常事件
type CostAnomaly struct {
	ID             int64   `bson:"id" json:"id"`
	Dimension      string  `bson:"dimension" json:"dimension"`
	DimensionValue string  `bson:"dimension_value" json:"dimension_value"`
	AnomalyDate    string  `bson:"anomaly_date" json:"anomaly_date"`
	ActualAmount   float64 `bson:"actual_amount" json:"actual_amount"`
	BaselineAmount float64 `bson:"baseline_amount" json:"baseline_amount"`
	DeviationPct   float64 `bson:"deviation_pct" json:"deviation_pct"`
	Severity       string  `bson:"severity" json:"severity"`
	PossibleCause  string  `bson:"possible_cause" json:"possible_cause"`
	TenantID       string  `bson:"tenant_id" json:"tenant_id"`
	CreateTime     int64   `bson:"ctime" json:"ctime"`
}

package domain

import "time"

// BudgetRule 预算规则
type BudgetRule struct {
	ID          int64                `bson:"id" json:"id"`
	Name        string               `bson:"name" json:"name"`
	AmountLimit float64              `bson:"amount_limit" json:"amount_limit"`
	Period      string               `bson:"period" json:"period"`
	ScopeType   string               `bson:"scope_type" json:"scope_type"`
	ScopeValue  string               `bson:"scope_value" json:"scope_value"`
	Thresholds  []float64            `bson:"thresholds" json:"thresholds"`
	NotifiedAt  map[string]time.Time `bson:"notified_at" json:"notified_at"`
	Status      string               `bson:"status" json:"status"`
	TenantID    string               `bson:"tenant_id" json:"tenant_id"`
	CreateTime  int64                `bson:"ctime" json:"ctime"`
	UpdateTime  int64                `bson:"utime" json:"utime"`
}

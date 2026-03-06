package domain

import "time"

// Recommendation 优化建议
type Recommendation struct {
	ID              int64      `bson:"id" json:"id"`
	Type            string     `bson:"type" json:"type"`
	Provider        string     `bson:"provider" json:"provider"`
	AccountID       int64      `bson:"account_id" json:"account_id"`
	ResourceID      string     `bson:"resource_id" json:"resource_id"`
	ResourceName    string     `bson:"resource_name" json:"resource_name"`
	Region          string     `bson:"region" json:"region"`
	Reason          string     `bson:"reason" json:"reason"`
	EstimatedSaving float64    `bson:"estimated_saving" json:"estimated_saving"`
	Status          string     `bson:"status" json:"status"`
	DismissedAt     *time.Time `bson:"dismissed_at" json:"dismissed_at"`
	DismissExpiry   *time.Time `bson:"dismiss_expiry" json:"dismiss_expiry"`
	TenantID        string     `bson:"tenant_id" json:"tenant_id"`
	CreateTime      int64      `bson:"ctime" json:"ctime"`
	UpdateTime      int64      `bson:"utime" json:"utime"`
}

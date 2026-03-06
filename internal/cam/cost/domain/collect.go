package domain

import "time"

// CollectLog 采集日志
type CollectLog struct {
	ID          int64     `bson:"id" json:"id"`
	AccountID   int64     `bson:"account_id" json:"account_id"`
	Provider    string    `bson:"provider" json:"provider"`
	Status      string    `bson:"status" json:"status"`
	StartTime   time.Time `bson:"start_time" json:"start_time"`
	EndTime     time.Time `bson:"end_time" json:"end_time"`
	BillStart   time.Time `bson:"bill_start" json:"bill_start"`
	BillEnd     time.Time `bson:"bill_end" json:"bill_end"`
	RecordCount int64     `bson:"record_count" json:"record_count"`
	Duration    int64     `bson:"duration_ms" json:"duration_ms"`
	ErrorMsg    string    `bson:"error_msg" json:"error_msg"`
	TenantID    string    `bson:"tenant_id" json:"tenant_id"`
	CreateTime  int64     `bson:"ctime" json:"ctime"`
}

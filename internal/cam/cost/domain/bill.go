package domain

import "time"

// UnifiedBill 统一账单模型
type UnifiedBill struct {
	ID              int64             `bson:"id" json:"id"`
	Provider        string            `bson:"provider" json:"provider"`
	AccountID       int64             `bson:"account_id" json:"account_id"`
	AccountName     string            `bson:"account_name" json:"account_name"`
	BillingStart    time.Time         `bson:"billing_start" json:"billing_start"`
	BillingEnd      time.Time         `bson:"billing_end" json:"billing_end"`
	ServiceType     string            `bson:"service_type" json:"service_type"`
	ServiceTypeName string            `bson:"service_type_name" json:"service_type_name"`
	ResourceID      string            `bson:"resource_id" json:"resource_id"`
	ResourceName    string            `bson:"resource_name" json:"resource_name"`
	Region          string            `bson:"region" json:"region"`
	Amount          float64           `bson:"amount" json:"amount"`
	Currency        string            `bson:"currency" json:"currency"`
	AmountCNY       float64           `bson:"amount_cny" json:"amount_cny"`
	ChargeType      string            `bson:"charge_type" json:"charge_type"`
	Tags            map[string]string `bson:"tags" json:"tags"`
	TenantID        string            `bson:"tenant_id" json:"tenant_id"`
	BillingDate     string            `bson:"billing_date" json:"billing_date"`
	CreateTime      int64             `bson:"ctime" json:"ctime"`
	UpdateTime      int64             `bson:"utime" json:"utime"`
}

// RawBillRecord 原始账单记录（审计用）
type RawBillRecord struct {
	ID          int64                  `bson:"id" json:"id"`
	AccountID   int64                  `bson:"account_id" json:"account_id"`
	Provider    string                 `bson:"provider" json:"provider"`
	RawData     map[string]interface{} `bson:"raw_data" json:"raw_data"`
	CollectID   string                 `bson:"collect_id" json:"collect_id"`
	BillingDate string                 `bson:"billing_date" json:"billing_date"`
	CreateTime  int64                  `bson:"ctime" json:"ctime"`
}

package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MappingStatus 平台证书↔云证书映射状态。
// 对齐 schema.sql cert_cloud_cert_mappings.status enum。
type MappingStatus string

const (
	// MappingStatusActive 映射生效（云侧证书在用）。
	MappingStatusActive MappingStatus = "active" // DEFAULT
	// MappingStatusOrphan 孤儿（旧云证书待清理，orphan-cleanup 任务消费）。
	MappingStatusOrphan MappingStatus = "orphan"
)

// CloudCertMapping 平台证书↔云证书 ID 映射文档（cert_cloud_cert_mappings）。
// uk_fp_cloud_account（certFingerprint+cloud+accountKey）两段式去重。
type CloudCertMapping struct {
	ID              primitive.ObjectID `bson:"_id,omitempty"`
	CertFingerprint string             `bson:"certFingerprint"` // ^[0-9a-f]{64}$
	Cloud           string             `bson:"cloud"`
	AccountKey      string             `bson:"accountKey"`
	CloudCertID     string             `bson:"cloudCertId"`
	UploadedAt      time.Time          `bson:"uploadedAt"` // DEFAULT=now()
	Status          MappingStatus      `bson:"status"`     // DEFAULT="active"
}

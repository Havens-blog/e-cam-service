package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// DiscoveryImportStatus 云端发现导入会话状态（cert-cloud-discovery-import 任务 2）。
// 对齐 schema.sql cert_discovery_import_sessions.status enum。
type DiscoveryImportStatus string

const (
	DiscoveryImportRunning       DiscoveryImportStatus = "running" // DEFAULT
	DiscoveryImportCompleted     DiscoveryImportStatus = "completed"
	DiscoveryImportPartialFailed DiscoveryImportStatus = "partial_failed"
)

// DiscoveryItemResult 发现导入单条目结果。
// 对齐 schema.sql cert_discovery_import_sessions.items[].result enum。
type DiscoveryItemResult string

const (
	DiscoveryItemPending DiscoveryItemResult = "pending"
	DiscoveryItemSuccess DiscoveryItemResult = "success"
	DiscoveryItemFailed  DiscoveryItemResult = "failed"
)

// DiscoveryImportItem 发现导入逐条目结果（部分失败不中断会话，失败条目可重跑，
// 条目幂等由 uk_fingerprint 与映射 Upsert 两段去重保证）。
// 条目以 cloud+accountKey+cloudCertId 三元组定位（区别于批量导入的 FileName
// 主键语义——伪装字段不允许，任务 2 实现注记）。
type DiscoveryImportItem struct {
	Cloud        string              `bson:"cloud"`
	AccountKey   string              `bson:"accountKey"`
	CloudCertID  string              `bson:"cloudCertId"`
	Result       DiscoveryItemResult `bson:"result"`
	MappedCertID string              `bson:"mappedCertId,omitempty"` // result=success 时有值（台账证书 ID）
	ErrorReason  string              `bson:"errorReason,omitempty"`  // result=failed 时错误码+静态文案
}

// DiscoveryImportProgress 发现导入进度（repository 写路径随条目完成原子递增）。
type DiscoveryImportProgress struct {
	Total     int `bson:"total"`     // >=1
	Succeeded int `bson:"succeeded"` // >=0
	Failed    int `bson:"failed"`    // >=0
}

// DiscoveryImportSession 云端发现导入会话文档（cert_discovery_import_sessions，
// TTL 30 天自动清理）。先持久化再异步执行（浏览器中断不丢结果，重开可见）；
// 任务 5 会话进度 GET 轮询数据源。会话整体限时 10 分钟口径由任务 4 编排层
// 承担（对齐 batchProcessTimeout），实体只承载状态。
type DiscoveryImportSession struct {
	ID         primitive.ObjectID      `bson:"_id,omitempty"`
	Status     DiscoveryImportStatus   `bson:"status"` // DEFAULT="running"
	Items      []DiscoveryImportItem   `bson:"items"`
	Progress   DiscoveryImportProgress `bson:"progress"`
	Operator   string                  `bson:"operator"`
	CreatedAt  time.Time               `bson:"createdAt"`            // DEFAULT=now()；TTL 基准
	FinishedAt *time.Time              `bson:"finishedAt,omitempty"` // DEFAULT=null
}

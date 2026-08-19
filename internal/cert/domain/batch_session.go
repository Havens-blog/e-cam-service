package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// BatchSessionStatus 批量导入会话状态。
// 对齐 schema.sql cert_batch_sessions.status enum。
type BatchSessionStatus string

const (
	BatchSessionRunning       BatchSessionStatus = "running" // DEFAULT
	BatchSessionCompleted     BatchSessionStatus = "completed"
	BatchSessionPartialFailed BatchSessionStatus = "partial_failed"
)

// BatchFileResult 批量导入单文件结果。
// 对齐 schema.sql cert_batch_sessions.files[].result enum。
type BatchFileResult string

const (
	BatchFilePending BatchFileResult = "pending"
	BatchFileSuccess BatchFileResult = "success"
	BatchFileFailed  BatchFileResult = "failed"
)

// BatchSessionFile 逐文件结果（部分失败不阻塞其他文件，失败文件可单独重试）。
type BatchSessionFile struct {
	FileName    string          `bson:"fileName"`
	Result      BatchFileResult `bson:"result"`
	CertID      string          `bson:"certId,omitempty"`      // result=success 时有值
	ErrorReason string          `bson:"errorReason,omitempty"` // result=failed 时错误码+详情
}

// BatchProgress 批量导入进度（repository 写路径随文件完成原子递增）。
type BatchProgress struct {
	Total  int `bson:"total"`  // >=1
	Done   int `bson:"done"`   // >=0
	Failed int `bson:"failed"` // >=0
}

// CertBatchSession 批量导入会话文档（cert_batch_sessions，TTL 30 天自动清理）。
// GET /api/v1/certs/batch/:batchId 进度轮询数据源。
type CertBatchSession struct {
	ID         primitive.ObjectID `bson:"_id,omitempty"`
	Status     BatchSessionStatus `bson:"status"` // DEFAULT="running"
	Files      []BatchSessionFile `bson:"files"`
	Progress   BatchProgress      `bson:"progress"`
	Operator   string             `bson:"operator"`
	CreatedAt  time.Time          `bson:"createdAt"`            // DEFAULT=now()；TTL 基准
	FinishedAt *time.Time         `bson:"finishedAt,omitempty"` // DEFAULT=null
}

package domain

import (
	"context"
	"time"
)

// TaskType 任务类型
type TaskType string

const (
	TaskTypeSyncAssets    TaskType = "sync_assets"     // 同步资产
	TaskTypeDiscoverAssets TaskType = "discover_assets" // 发现资产
)

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"   // 待执行
	TaskStatusRunning   TaskStatus = "running"   // 执行中
	TaskStatusCompleted TaskStatus = "completed" // 已完成
	TaskStatusFailed    TaskStatus = "failed"    // 失败
	TaskStatusCancelled TaskStatus = "cancelled" // 已取消
)

// Task 任务
type Task struct {
	ID          string                 `json:"id" bson:"_id"`                  // 任务ID
	Type        TaskType               `json:"type" bson:"type"`               // 任务类型
	Status      TaskStatus             `json:"status" bson:"status"`           // 任务状态
	Params      map[string]interface{} `json:"params" bson:"params"`           // 任务参数
	Result      map[string]interface{} `json:"result,omitempty" bson:"result"` // 任务结果
	Error       string                 `json:"error,omitempty" bson:"error"`   // 错误信息
	Progress    int                    `json:"progress" bson:"progress"`       // 进度 (0-100)
	Message     string                 `json:"message" bson:"message"`         // 当前消息
	CreatedBy   string                 `json:"created_by" bson:"created_by"`   // 创建者
	CreatedAt   time.Time              `json:"created_at" bson:"created_at"`   // 创建时间
	StartedAt   *time.Time             `json:"started_at,omitempty" bson:"started_at"`   // 开始时间
	CompletedAt *time.Time             `json:"completed_at,omitempty" bson:"completed_at"` // 完成时间
	Duration    int64                  `json:"duration,omitempty" bson:"duration"` // 执行时长（秒）
}

// TaskExecutor 任务执行器接口
type TaskExecutor interface {
	// Execute 执行任务
	Execute(ctx context.Context, task *Task) error
	
	// GetType 获取任务类型
	GetType() TaskType
}

// TaskFilter 任务查询过滤器
type TaskFilter struct {
	Type      TaskType   `json:"type"`
	Status    TaskStatus `json:"status"`
	CreatedBy string     `json:"created_by"`
	StartDate time.Time  `json:"start_date"`
	EndDate   time.Time  `json:"end_date"`
	Offset    int64      `json:"offset"`
	Limit     int64      `json:"limit"`
}

// SyncAssetsParams 同步资产任务参数
type SyncAssetsParams struct {
	Provider   string   `json:"provider"`
	AssetTypes []string `json:"asset_types"`
	Regions    []string `json:"regions"`
	AccountID  int64    `json:"account_id"`
}

// SyncAssetsResult 同步资产任务结果
type SyncAssetsResult struct {
	TotalCount     int                    `json:"total_count"`
	AddedCount     int                    `json:"added_count"`
	UpdatedCount   int                    `json:"updated_count"`
	DeletedCount   int                    `json:"deleted_count"`
	UnchangedCount int                    `json:"unchanged_count"`
	ErrorCount     int                    `json:"error_count"`
	Errors         []string               `json:"errors,omitempty"`
	Details        map[string]interface{} `json:"details,omitempty"`
}

// DiscoverAssetsParams 发现资产任务参数
type DiscoverAssetsParams struct {
	Provider   string   `json:"provider"`
	Region     string   `json:"region"`
	AssetTypes []string `json:"asset_types"`
	AccountID  int64    `json:"account_id"`
}

// DiscoverAssetsResult 发现资产任务结果
type DiscoverAssetsResult struct {
	Count  int                    `json:"count"`
	Assets []interface{}          `json:"assets"`
	Details map[string]interface{} `json:"details,omitempty"`
}

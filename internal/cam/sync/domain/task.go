package domain

import "time"

// SyncTaskStatus 同步任务状态
type SyncTaskStatus string

const (
	TaskStatusPending   SyncTaskStatus = "pending"   // 待执行
	TaskStatusRunning   SyncTaskStatus = "running"   // 执行中
	TaskStatusSuccess   SyncTaskStatus = "success"   // 成功
	TaskStatusFailed    SyncTaskStatus = "failed"    // 失败
	TaskStatusCancelled SyncTaskStatus = "cancelled" // 已取消
)

// SyncTask 同步任务
type SyncTask struct {
	ID              int64          `json:"id" bson:"id"`
	AccountID       int64          `json:"account_id" bson:"account_id"`           // 云账号ID
	Provider        CloudProvider  `json:"provider" bson:"provider"`               // 云厂商
	ResourceType    string         `json:"resource_type" bson:"resource_type"`     // 资源类型
	Region          string         `json:"region" bson:"region"`                   // 地域
	Status          SyncTaskStatus `json:"status" bson:"status"`                   // 任务状态
	StartTime       int64          `json:"start_time" bson:"start_time"`           // 开始时间
	EndTime         int64          `json:"end_time" bson:"end_time"`               // 结束时间
	Duration        int64          `json:"duration" bson:"duration"`               // 执行时长(秒)
	TotalCount      int            `json:"total_count" bson:"total_count"`         // 总资源数
	AddedCount      int            `json:"added_count" bson:"added_count"`         // 新增数量
	UpdatedCount    int            `json:"updated_count" bson:"updated_count"`     // 更新数量
	DeletedCount    int            `json:"deleted_count" bson:"deleted_count"`     // 删除数量
	UnchangedCount  int            `json:"unchanged_count" bson:"unchanged_count"` // 未变化数量
	ErrorCount      int            `json:"error_count" bson:"error_count"`         // 错误数量
	ErrorMessage    string         `json:"error_message" bson:"error_message"`     // 错误信息
	Ctime           int64          `json:"ctime" bson:"ctime"`                     // 创建时间
	Utime           int64          `json:"utime" bson:"utime"`                     // 更新时间
}

// SyncResult 同步结果
type SyncResult struct {
	TaskID         int64
	Success        bool
	TotalCount     int
	AddedCount     int
	UpdatedCount   int
	DeletedCount   int
	UnchangedCount int
	ErrorCount     int
	Errors         []SyncError
	Duration       time.Duration
}

// SyncError 同步错误
type SyncError struct {
	ResourceID string
	Error      string
	Timestamp  time.Time
}

// Start 开始任务
func (t *SyncTask) Start() {
	t.Status = TaskStatusRunning
	t.StartTime = time.Now().Unix()
	t.Utime = time.Now().Unix()
}

// Complete 完成任务
func (t *SyncTask) Complete(result *SyncResult) {
	t.Status = TaskStatusSuccess
	t.EndTime = time.Now().Unix()
	t.Duration = t.EndTime - t.StartTime
	t.TotalCount = result.TotalCount
	t.AddedCount = result.AddedCount
	t.UpdatedCount = result.UpdatedCount
	t.DeletedCount = result.DeletedCount
	t.UnchangedCount = result.UnchangedCount
	t.ErrorCount = result.ErrorCount
	t.Utime = time.Now().Unix()
}

// Fail 任务失败
func (t *SyncTask) Fail(err error) {
	t.Status = TaskStatusFailed
	t.EndTime = time.Now().Unix()
	t.Duration = t.EndTime - t.StartTime
	t.ErrorMessage = err.Error()
	t.Utime = time.Now().Unix()
}

// Cancel 取消任务
func (t *SyncTask) Cancel() {
	t.Status = TaskStatusCancelled
	t.EndTime = time.Now().Unix()
	t.Duration = t.EndTime - t.StartTime
	t.Utime = time.Now().Unix()
}

// IsRunning 判断任务是否正在运行
func (t *SyncTask) IsRunning() bool {
	return t.Status == TaskStatusRunning
}

// IsCompleted 判断任务是否已完成
func (t *SyncTask) IsCompleted() bool {
	return t.Status == TaskStatusSuccess || t.Status == TaskStatusFailed || t.Status == TaskStatusCancelled
}

// GetSuccessRate 获取成功率
func (t *SyncTask) GetSuccessRate() float64 {
	if t.TotalCount == 0 {
		return 0
	}
	successCount := t.AddedCount + t.UpdatedCount + t.UnchangedCount
	return float64(successCount) / float64(t.TotalCount) * 100
}

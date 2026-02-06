// Package web 任务 HTTP 处理器
package web

import "time"

// SubmitSyncAssetsTaskReq 提交同步资产任务请求
type SubmitSyncAssetsTaskReq struct {
	Provider   string   `json:"provider" binding:"required"`
	AssetTypes []string `json:"asset_types"`
	Regions    []string `json:"regions"`
	AccountID  int64    `json:"account_id"`
}

// SubmitDiscoverAssetsTaskReq 提交发现资产任务请求
type SubmitDiscoverAssetsTaskReq struct {
	Provider   string   `json:"provider" binding:"required"`
	Region     string   `json:"region"`
	AssetTypes []string `json:"asset_types"`
	AccountID  int64    `json:"account_id"`
}

// TaskResp 任务响应
type TaskResp struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Status      string                 `json:"status"`
	Params      map[string]interface{} `json:"params"`
	Result      map[string]interface{} `json:"result,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Progress    int                    `json:"progress"`
	Message     string                 `json:"message"`
	CreatedBy   string                 `json:"created_by"`
	CreatedAt   time.Time              `json:"created_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Duration    int64                  `json:"duration,omitempty"`
}

// TaskListResp 任务列表响应
type TaskListResp struct {
	Tasks []TaskResp `json:"tasks"`
	Total int64      `json:"total"`
}

// SubmitTaskResp 提交任务响应
type SubmitTaskResp struct {
	TaskID  string `json:"task_id"`
	Message string `json:"message"`
}

package task

import "github.com/Havens-blog/e-cam-service/pkg/taskx"

// 定义 CAM 模块的任务类型
const (
	TaskTypeSyncAssets     taskx.TaskType = "cam:sync_assets"
	TaskTypeDiscoverAssets taskx.TaskType = "cam:discover_assets"
)

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
	Count   int                    `json:"count"`
	Assets  []interface{}          `json:"assets"`
	Details map[string]interface{} `json:"details,omitempty"`
}

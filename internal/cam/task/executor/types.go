package executor

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

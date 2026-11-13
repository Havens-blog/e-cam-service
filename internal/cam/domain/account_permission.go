package domain

import "time"

// 权限体检单项状态
const (
	// PermissionStatusOK 该资源类型可正常列举
	PermissionStatusOK = "ok"
	// PermissionStatusDenied 明确权限不足（403/AccessDenied/NoPermission 等）
	PermissionStatusDenied = "denied"
	// PermissionStatusError 其他失败（网络、限流、参数等），不代表没有权限
	PermissionStatusError = "error"
	// PermissionStatusUnsupported 该云厂商暂不支持探测
	PermissionStatusUnsupported = "unsupported"
)

// PermissionItem 单个资源类型的权限体检结果
type PermissionItem struct {
	AssetType string `json:"asset_type" bson:"asset_type"` // 资源类型标识，如 ecs
	AssetName string `json:"asset_name" bson:"asset_name"` // 资源类型名称，如 云服务器 ECS
	Status    string `json:"status" bson:"status"`         // ok / denied / error / unsupported
	Code      string `json:"code" bson:"code"`             // 云厂商错误码，如 Forbidden
	Message   string `json:"message" bson:"message"`       // 可读说明
}

// PermissionSummary 权限体检汇总
type PermissionSummary struct {
	OK          int `json:"ok" bson:"ok"`
	Denied      int `json:"denied" bson:"denied"`
	Error       int `json:"error" bson:"error"`
	Unsupported int `json:"unsupported" bson:"unsupported"`
}

// PermissionCheck 云账号权限体检快照
type PermissionCheck struct {
	AccountID        int64             `json:"account_id" bson:"account_id"`
	Provider         string            `json:"provider" bson:"provider"`
	IdentityType     string            `json:"identity_type" bson:"identity_type"`           // RAMUser / RAMRole / PrimaryAccount / 空(未识别)
	IsPrimaryAccount bool              `json:"is_primary_account" bson:"is_primary_account"` // 是否主账号 AK
	Items            []PermissionItem  `json:"items" bson:"items"`
	Summary          PermissionSummary `json:"summary" bson:"summary"`
	CheckedAt        time.Time         `json:"checked_at" bson:"checked_at"`
}

// DeniedCount 权限不足项数
func (p *PermissionCheck) DeniedCount() int {
	if p == nil {
		return 0
	}
	return p.Summary.Denied
}

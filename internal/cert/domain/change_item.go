package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Channel 执行通道类型。对齐 schema.sql cert_change_items.resourceRef.channel enum。
type Channel string

const (
	ChannelCloudAPI Channel = "cloud_api"
	ChannelK8sAPI   Channel = "k8s_api"
)

// ChangeAction 变更项动作。对齐 schema.sql cert_change_items.action enum。
type ChangeAction string

const (
	// ActionUploadAndBind 两段式：上传云证书库 + 绑定目标资源（cloud_api 通道）。
	ActionUploadAndBind ChangeAction = "upload_and_bind"
	// ActionPatchCRD patch K8s CRD 证书引用字段（k8s_api 通道）。
	ActionPatchCRD ChangeAction = "patch_crd"
)

// ChangeItemStatus 变更项状态（异步子任务状态载体，非 HTTP 响应）。
// 对齐 schema.sql cert_change_items.status enum。
type ChangeItemStatus string

const (
	ItemStatusPending     ChangeItemStatus = "pending" // DEFAULT
	ItemStatusRunning     ChangeItemStatus = "running"
	ItemStatusSuccess     ChangeItemStatus = "success"
	ItemStatusFailed      ChangeItemStatus = "failed"
	ItemStatusRateLimited ChangeItemStatus = "rate_limited" // 云 API 限流，退避重试中
	ItemStatusRolledBack  ChangeItemStatus = "rolled_back"
	// ItemStatusRollbackFailed 回滚失败（任务 5.8）：通道 Rollback 动作自身失败，
	// 立即告警（四类之"回滚失败"）转人工；订单收敛 rollback_failed 终态。
	ItemStatusRollbackFailed ChangeItemStatus = "rollback_failed"
	ItemStatusSkipped        ChangeItemStatus = "skipped" // 不可自动变更/人工取消
)

// ResourceRef 持久化完整 DeployTarget（cert_change_items.resourceRef）。
// 按 action 分支必填（schema.sql anyOf 校验器强制）：
//   - upload_and_bind(cloud_api): channel+cloud+product+accountKey+resourceId
//   - patch_crd(k8s_api):        channel+clusterId+namespace+kind+resourceId
//
// 异步子任务仅凭持久化数据即可重构 DeployTarget，不回查台账/快照。
type ResourceRef struct {
	Channel    Channel `bson:"channel"`
	Cloud      string  `bson:"cloud,omitempty"`      // cloud_api 必填
	Product    string  `bson:"product,omitempty"`    // cdn/dcdn/waf/alb/clb/nlb；cloud_api 必填
	AccountKey string  `bson:"accountKey,omitempty"` // cloud_api 必填
	ClusterID  string  `bson:"clusterId,omitempty"`  // k8s_api 必填
	Namespace  string  `bson:"namespace,omitempty"`  // k8s_api 必填（CRD 所在命名空间）
	Kind       string  `bson:"kind,omitempty"`       // k8s_api 必填（CRD kind）
	ResourceID string  `bson:"resourceId"`           // 云资源 ID 或 CRD 实例名，必填
}

// ChangeItem 变更项文档（cert_change_items，逐项执行）。
type ChangeItem struct {
	ID             primitive.ObjectID `bson:"_id,omitempty"`
	OrderID        string             `bson:"orderId"`
	BatchNo        int                `bson:"batchNo,omitempty"` // 批次归属，Confirm 时固化；>=1
	Action         ChangeAction       `bson:"action"`
	ResourceRef    ResourceRef        `bson:"resourceRef"`
	OldCloudCertID string             `bson:"oldCloudCertId,omitempty"` // 回滚依据
	NewCloudCertID string             `bson:"newCloudCertId,omitempty"`
	Status         ChangeItemStatus   `bson:"status"`                // DEFAULT="pending"
	Error          string             `bson:"error,omitempty"`       // 失败错误码+详情
	HeartbeatAt    *time.Time         `bson:"heartbeatAt,omitempty"` // 执行心跳（30s 间隔更新）
	ExecutedAt     *time.Time         `bson:"executedAt,omitempty"`  // DEFAULT=null；领取执行权时固化（crd-recheck 延迟基准）
	// RecheckedAt crd-recheck 单轮复检完成时点（任务 5.9）：单轮复检幂等标记——
	// 仅 success 且 recheckedAt 缺失的 patch_crd 项进入消费；通过/失败均固化，
	// 保证复检次数固定 1（Hard Rule：失败不自动二次复检）。schema.sql 校验器无
	// additionalProperties 约束，新增可选字段向后兼容。
	RecheckedAt *time.Time `bson:"recheckedAt,omitempty"`
}

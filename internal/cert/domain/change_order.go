package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ChangeStatus 变更单状态（9 态状态机）。
// 对齐 schema.sql cert_change_orders.status enum；
// 状态迁移逻辑在变更域任务实现，仓储层仅提供原子迁移原语。
type ChangeStatus string

const (
	ChangeStatusDraft            ChangeStatus = "draft"
	ChangeStatusPendingConfirm   ChangeStatus = "pending_confirm"
	ChangeStatusExecuting        ChangeStatus = "executing"
	ChangeStatusVerifying        ChangeStatus = "verifying"
	ChangeStatusCompleted        ChangeStatus = "completed"
	ChangeStatusPartialCompleted ChangeStatus = "partial_completed"
	ChangeStatusRolledBack       ChangeStatus = "rolled_back"
	ChangeStatusRollbackFailed   ChangeStatus = "rollback_failed"
	ChangeStatusCancelled        ChangeStatus = "cancelled" // 取消终态
)

// ActiveChangeStatuses 活跃态集合（持有 activeMutex）：
// 待确认/执行中/验证中。判定辅助供状态机与预检查使用。
var ActiveChangeStatuses = []ChangeStatus{
	ChangeStatusDraft,
	ChangeStatusPendingConfirm,
	ChangeStatusExecuting,
	ChangeStatusVerifying,
}

// TerminalChangeStatuses 终态集合（迁移时同原子 update 清除 activeMutex）。
var TerminalChangeStatuses = []ChangeStatus{
	ChangeStatusCompleted,
	ChangeStatusPartialCompleted,
	ChangeStatusRolledBack,
	ChangeStatusRollbackFailed,
	ChangeStatusCancelled,
}

// IsActiveChangeStatus 判断是否活跃态（持有在途互斥 token）。
func IsActiveChangeStatus(s ChangeStatus) bool {
	for _, v := range ActiveChangeStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// BatchInfo 分批灰度信息（分批单在 Confirm 时固化批次分配）。
// 对齐 schema.sql cert_change_orders.batchInfo。
type BatchInfo struct {
	TotalBatches int        `bson:"totalBatches"`       // >=1
	CurrentBatch int        `bson:"currentBatch"`       // >=1；执行仅取 batchNo=currentBatch 的项
	BatchSize    int        `bson:"batchSize"`          // >=1
	Paused       bool       `bson:"paused,omitempty"`   // DEFAULT=false，批间暂停待人工续批
	PausedAt     *time.Time `bson:"pausedAt,omitempty"` // 暂停起始，pauseTimeoutHours 超时自动取消基准
}

// VerifyExpected 验证窗口预期终态快照（批执行完成时固化；分批单每批按该批域名刷新）。
// domains 构建时剔除豁免清单命中的域名并记入 excludedDomains——豁免域名不参与达标判定。
type VerifyExpected struct {
	NewCertFingerprint string    `bson:"newCertFingerprint"` // ^[0-9a-f]{64}$
	Domains            []string  `bson:"domains"`
	ExcludedDomains    []string  `bson:"excludedDomains,omitempty"` // 构建时剔除的豁免域名；计 skipped
	WindowUntil        time.Time `bson:"windowUntil"`
}

// ChangeOrder 变更单文档（cert_change_orders）。
// activeMutex=在途互斥 token：活跃态=oldCertFingerprint，终态 $unset；
// uk_active_mutex 部分唯一索引强制同一指纹同时仅一张活跃单。
type ChangeOrder struct {
	ID                 primitive.ObjectID `bson:"_id,omitempty"`
	OldCertFingerprint string             `bson:"oldCertFingerprint"` // ^[0-9a-f]{64}$
	NewCertID          string             `bson:"newCertId"`
	Status             ChangeStatus       `bson:"status"`
	BatchInfo          *BatchInfo         `bson:"batchInfo,omitempty"`         // DEFAULT=null（未分批）
	SnapshotID         string             `bson:"snapshotId"`                  // 绑定的扫描快照（确认时点重校验依据）
	VerifyWindowUntil  *time.Time         `bson:"verifyWindowUntil,omitempty"` // 每批进入 verifying 时刷新
	ProtectUntil       *time.Time         `bson:"protectUntil,omitempty"`      // 回滚保护期截止
	ActiveMutex        string             `bson:"activeMutex,omitempty"`       // 活跃态=oldCertFingerprint，终态清除
	VerifyExpected     *VerifyExpected    `bson:"verifyExpected,omitempty"`
	Creator            string             `bson:"creator"`
	CreatedAt          time.Time          `bson:"createdAt"` // DEFAULT=now()
}

package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ScanStatus 扫描快照状态。
// 对齐 schema.sql cert_scan_snapshots.status enum；
// running 超 thresholds.scanTimeoutHours 由 scan-timeout 任务转 failed（释放防重锁）。
type ScanStatus string

const (
	ScanStatusRunning ScanStatus = "running"
	ScanStatusDone    ScanStatus = "done"
	ScanStatusFailed  ScanStatus = "failed"
)

// FailReasonScanTimedOut running 超时转 failed 的原因码（scan-timeout 任务）。
const FailReasonScanTimedOut = "SCAN_TIMED_OUT"

// 扫描终态失败原因码（任务 3.5）。
const (
	// FailReasonScanDiscoveryFailed 全部发现通道失败（整体失败）。
	FailReasonScanDiscoveryFailed = "SCAN_DISCOVERY_FAILED"
	// FailReasonScanNoChannels 无可扫描通道（无 active 云账号且无 enabled CRD 登记）——
	// 空范围快照会使引用三态的"已扫描"声明失真，显式失败而非静默成功。
	FailReasonScanNoChannels = "SCAN_NO_CHANNELS"
	// FailReasonScanWriteFailed 引用批量落库失败（存储层故障，整体失败）。
	FailReasonScanWriteFailed = "SCAN_WRITE_FAILED"
)

// CoverageMeta 单云×产品覆盖率条目（分母来源 internal/asset 资产盘点）。
// total=-1 表示分母不可用（盲区声明）；covered 与 total 为异构时点数据，不强制 covered<=total。
type CoverageMeta struct {
	Cloud   string `bson:"cloud"`
	Product string `bson:"product"`
	Covered int    `bson:"covered"` // 本轮发现引用的去重资源数，>=0
	Total   int    `bson:"total"`   // 资产盘点在用资源数；-1=分母不可用；K8s crd 恒 -1（asset 不盘点 K8s）
	// Lagging asset 盘点滞后标记（任务 3.5）：covered>total 时置位——covered 与 total
	// 为异构时点数据，以 covered 为准（EffectiveTotal），视图输出"asset 盘点滞后"警告。
	Lagging bool `bson:"lagging,omitempty"`
}

// EffectiveTotal 有效分母：covered>total（asset 盘点滞后）时以 covered 为准，
// 防止覆盖率展示超过 100%（tech-design"覆盖率分母"一致性条款）。
func (m CoverageMeta) EffectiveTotal() int {
	if m.Total >= 0 && m.Total < m.Covered {
		return m.Covered
	}
	return m.Total
}

// ScanChannelFailure 扫描通道失败记录（任务 3.5）：某云/产品通道失败时写入快照
// 元数据（部分失败不阻塞其他云），供视图/看板声明盲区。Reason 为静态文案+安全
// 参数（云/产品/账号名、错误码），不含任何凭证或私钥片段。
type ScanChannelFailure struct {
	Cloud   string `bson:"cloud"`             // 云名；K8s 通道为空
	Product string `bson:"product"`           // 产品；K8s 通道为 crd
	Account string `bson:"account,omitempty"` // 云账号名 / K8s 集群名
	Reason  string `bson:"reason"`            // 失败原因（安全参数）
}

// ScanSnapshot 扫描快照元数据文档（cert_scan_snapshots）。
// 扫描新鲜度派生基准：freshness = now - startedAt。
type ScanSnapshot struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	StartedAt    time.Time          `bson:"startedAt"`            // DEFAULT=now()
	FinishedAt   *time.Time         `bson:"finishedAt,omitempty"` // DEFAULT=null，运行中无值
	Status       ScanStatus         `bson:"status"`               // DEFAULT="running"
	FailReason   string             `bson:"failReason,omitempty"` // status=failed 时的原因码
	CoverageMeta []CoverageMeta     `bson:"coverageMeta,omitempty"`
	// PartialFailures 部分失败通道清单（任务 3.5）：某云/产品失败不阻塞其他云，
	// 记入快照元数据；全部通道失败时整体转 failed。
	PartialFailures []ScanChannelFailure `bson:"partialFailures,omitempty"`
}

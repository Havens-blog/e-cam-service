package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ProbeStatus TLS 探测结果状态。
// 对齐 schema.sql cert_probe_results.status enum（6 值）。
type ProbeStatus string

const (
	// ProbeStatusConsistent 线上指纹与台账一致。
	ProbeStatusConsistent ProbeStatus = "consistent"
	// ProbeStatusDiff 常规差异（线上≠台账，走常规差异告警通道）。
	ProbeStatusDiff ProbeStatus = "diff"
	// ProbeStatusChangeLinkedDiff 验证窗口内变更关联差异（预期切换，走变更关联通道）。
	ProbeStatusChangeLinkedDiff ProbeStatus = "change_linked_diff"
	// ProbeStatusUnreachable 不可达。
	ProbeStatusUnreachable ProbeStatus = "unreachable"
	// ProbeStatusExempt 豁免清单命中（仍探测但不告警）。
	ProbeStatusExempt ProbeStatus = "exempt"
	// ProbeStatusWildcardSkipped 通配符 SAN 默认跳过拨测（可经 wildcardProbeOverrides 指定子域名替代探测）。
	ProbeStatusWildcardSkipped ProbeStatus = "wildcard_skipped"
)

// ProbeResult TLS 探测结果文档（cert_probe_results，TTL 90 天自动清理）。
type ProbeResult struct {
	ID                primitive.ObjectID `bson:"_id,omitempty"`
	Domain            string             `bson:"domain"`
	ProbeAt           time.Time          `bson:"probeAt"`                     // DEFAULT=now()；TTL 基准
	OnlineFingerprint string             `bson:"onlineFingerprint,omitempty"` // unreachable 时缺省
	OnlineNotAfter    *time.Time         `bson:"onlineNotAfter,omitempty"`
	Status            ProbeStatus        `bson:"status"`
	ChangeOrderID     string             `bson:"changeOrderId,omitempty"` // change_linked_diff 时关联变更单
}

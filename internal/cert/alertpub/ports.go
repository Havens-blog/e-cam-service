// Package alertpub 证书域告警发布实现（cert 自持）。
//
// 背景（cert 服务抽取 · 绞杀者 step 1）：本包承载原先寄居在 internal/alert 的
// 证书告警逻辑（webhook 载荷/发送、路由判定、severity 映射、退避重试、投递
// 记录），使其回归 cert 域。本包对 internal/alert 零 import——所需的通用基建
// （SMTP 邮件发送、投递记录持久化）经下列窄端口注入，实现由组合根（ioc）用
// alert 的既有基建适配。cert 域从此不反向依赖 alert，反之亦然，为后续将 cert
// 整体抽为独立服务扫清唯一的结构性阻碍。
package alertpub

import "context"

// Severity 告警级别（cert 自有字符串类型；取值与 alert domain.Severity 一致：
// info/warning/critical——跨端口以字符串传递，组合根 adapter 负责与 alert 枚举
// 互转，本包不 import alert）。
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// EmailSink 邮件投递端口（消费方接口）。实现由组合根基于 alert 通用 EmailSender
// + SMTP 配置适配；SMTP 未配置时注入 nil，发布器据此停用邮件通道（webhook 不受
// 影响）。severity 以字符串传递。
type EmailSink interface {
	SendCertEmail(ctx context.Context, title, body string, severity string, to []string) error
}

// DeliveryRecord 投递记录（领域无关中性结构）。组合根将其映射为 alert 的
// AlertEvent 并经 AlertDAO 终态直写 ecam_alert_event（归属推迟到接口背后：
// cert 抽为独立服务时换实现即可，不改本包）。
type DeliveryRecord struct {
	Type     string         // 告警类型标识（如 cert_expiry）
	Severity string         // info/warning/critical
	Title    string         // 人读标题
	Content  map[string]any // 白名单键（不含 webhook URL/凭证片段）
	Source   string         // 事件来源（如 cert_alert:expiry）
	Status   string         // sent | failed
	SentAt   *int64         // 送达时间 UnixMilli（status=sent 时非 nil）
}

// DeliveryRecorder 投递记录端口（消费方接口）。记录失败不影响投递结果语义
// （实现侧仅告警）。
type DeliveryRecorder interface {
	Record(ctx context.Context, rec DeliveryRecord) error
}

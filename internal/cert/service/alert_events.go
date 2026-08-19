package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// ---------------------------------------------------------------------
// 四类业务告警事件模型与统一发布接口（任务 4.2；PRD Monitoring Requirements）
// ---------------------------------------------------------------------

// AlertCategory 业务告警四类（PRD Monitoring：到期分级/TLS 差异/变更关联/回滚失败）。
//
// Hard Rule：四类区分以 category 字段承载，alert 通道不得把到期分级当差异类处理。
type AlertCategory string

const (
	// AlertCategoryExpiry 到期分级（本任务去重状态机产出，天级巡检，仅升级触发）。
	AlertCategoryExpiry AlertCategory = "expiry"
	// AlertCategoryTLSDiff TLS 差异（常规 diff，探测事件触发，4.3 通道消费）。
	AlertCategoryTLSDiff AlertCategory = "tls_diff"
	// AlertCategoryChangeLinked 变更关联（验证窗口内 change_linked_diff，
	// 走 verifyWindowRoute 通道，附 orderId）。
	AlertCategoryChangeLinked AlertCategory = "change_linked"
	// AlertCategoryRollbackFailed 回滚失败（Rollback 自身失败立即告警转人工）。
	AlertCategoryRollbackFailed AlertCategory = "rollback_failed"

	// AlertCategoryTest 渠道测试告警（任务 4.5 POST /settings/test 发送）。
	// 非业务告警：仅用于告警接收人验证渠道连通性与 channelConfirmed 确认，
	// 不计入 PRD 四类业务告警口径。
	AlertCategoryTest AlertCategory = "test"
)

// CertAlertEvent 证书域业务告警事件统一模型（四类共用，经 PublishAlert 发布）。
//
// 硬约束：按证书粒度聚合内容，不含私钥/凭证片段——本结构体不承载任何私钥字段，
// 到期分级事件仅携带指纹/SAN 摘要/级别/daysLeft/到期时间。
type CertAlertEvent struct {
	Category    AlertCategory           // 四类之一（必填）
	Title       string                  // 人读标题（通道渲染用）
	Fingerprint string                  // 关联证书 SHA256 指纹；change_linked/rollback_failed 关联订单时可空
	SANs        []string                // SAN 摘要（expiry 事件按证书粒度聚合）
	Level       domain.ExpiryAlertLevel // Category=expiry 时的分级（none/L30/L14/L7/expired）
	DaysLeft    int                     // Category=expiry 时的剩余天数（ceil 口径）
	NotAfter    time.Time               // Category=expiry 时的证书到期时间
	Domain      string                  // tls_diff/change_linked 关联域名
	OrderID     string                  // change_linked/rollback_failed 关联变更单号
	Detail      string                  // 机器可读补充（不得含私钥/凭证片段）
	At          time.Time               // 事件产生时间（发布方填充）

	// VerifyWindow 验证窗口路由判定入参（任务 4.3 增量字段，additive）：
	// change_linked 事件由 5.10 填充窗口上下文（专用通道路由/预期指纹/达标计数）；
	// 其余类别恒为 nil，既有发布方零改动。nil 语义见 VerifyWindowContext 注释。
	VerifyWindow *VerifyWindowContext
}

// CertAlertPublisher 四类业务告警统一发布接口。
//
// 实现方：4.3 alert 通道（internal/alert webhook+email，按 category 分流路由）；
// 本任务提供内存/日志两个默认实现（通道落地前的默认发布路径与测试注入缝）。
type CertAlertPublisher interface {
	// PublishAlert 发布单条告警事件；返回 error 供调用方决定重试语义
	//（到期分级引擎按 at-least-once 处理：发布失败不落去重状态，下轮巡检重发）。
	PublishAlert(ctx context.Context, event CertAlertEvent) error
}

// ---------------------------------------------------------------------
// 默认实现一：内存记录（测试注入缝 + 4.3 通道桥接前的默认收集器）
// ---------------------------------------------------------------------

// InMemoryAlertPublisher 内存发布实现：线程安全记录事件供测试断言/桥接消费。
type InMemoryAlertPublisher struct {
	mu     sync.Mutex
	events []CertAlertEvent
}

// NewInMemoryAlertPublisher 创建内存发布器。
func NewInMemoryAlertPublisher() *InMemoryAlertPublisher {
	return &InMemoryAlertPublisher{}
}

// PublishAlert 记录事件副本，恒成功。
func (p *InMemoryAlertPublisher) PublishAlert(_ context.Context, event CertAlertEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	stored := event
	stored.SANs = append([]string(nil), event.SANs...)
	p.events = append(p.events, stored)
	return nil
}

// Events 返回已记录事件的深拷贝（外部修改不影响内部状态）。
func (p *InMemoryAlertPublisher) Events() []CertAlertEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]CertAlertEvent, len(p.events))
	for i, e := range p.events {
		out[i] = e
		out[i].SANs = append([]string(nil), e.SANs...)
	}
	return out
}

// ---------------------------------------------------------------------
// 默认实现二：结构化日志（4.3 通道落地前的默认发布路径）
// ---------------------------------------------------------------------

// loggingAlertPublisher 日志发布实现：结构化日志输出事件四类要素，恒成功。
type loggingAlertPublisher struct{}

// NewLoggingAlertPublisher 创建日志发布器（InspectLedger 等 4.4 装配未接通道时的默认）。
func NewLoggingAlertPublisher() CertAlertPublisher {
	return &loggingAlertPublisher{}
}

// PublishAlert 输出结构化日志；仅记录事件字段，不携带任何私钥/凭证片段。
func (p *loggingAlertPublisher) PublishAlert(_ context.Context, event CertAlertEvent) error {
	slog.Info("cert alert published",
		slog.String("category", string(event.Category)),
		slog.String("title", event.Title),
		slog.String("fingerprint", event.Fingerprint),
		slog.String("level", string(event.Level)),
		slog.Int("daysLeft", event.DaysLeft),
		slog.String("domain", event.Domain),
		slog.String("orderId", event.OrderID),
		slog.String("detail", event.Detail),
	)
	return nil
}

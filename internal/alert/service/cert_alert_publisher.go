// Package service — 证书域告警通道接线（任务 4.3）。
// 消费 4.2 CertAlertEvent（cert/service.CertAlertPublisher 实现）：
// webhook POST JSON + email 两组接收人按 cert AlertConfig 触达；
// verifyWindowRoute 窗口路由；发送失败有界退避重试；最终失败落
// ecam_alert_event 投递记录（状态+原因，供补发/查询）。
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/alert/channel"
	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
	"github.com/Havens-blog/e-cam-service/internal/alert/repository/dao"
	certdomain "github.com/Havens-blog/e-cam-service/internal/cert/domain"
	certservice "github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/gotomicro/ego/core/elog"
	"github.com/spf13/viper"
)

// ---------------------------------------------------------------------
// SMTP 配置（应用 config 注入；缺省启动警告而非失败）
// ---------------------------------------------------------------------

// CertSMTPConfig 证书域邮件通道 SMTP 凭据。来源：应用 config 键
// alert.cert_smtp（host/port/user/pass/from）——tech-design Open Questions
// "告警渠道凭据来源待配置"的落点：随应用配置注入，不进业务库。
type CertSMTPConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"` // 0 视为默认 25
	User string `mapstructure:"user"`
	Pass string `mapstructure:"pass"`
	From string `mapstructure:"from"`
}

// defaultCertSMTPPort SMTP 缺省端口。
const defaultCertSMTPPort = 25

// LoadCertSMTPConfig 读取 alert.cert_smtp；键缺省时返回零值
// （邮件通道停用 + 构造方启动警告，不由配置读取失败阻塞启动）。
func LoadCertSMTPConfig() CertSMTPConfig {
	var cfg CertSMTPConfig
	if err := viper.UnmarshalKey("alert.cert_smtp", &cfg); err != nil {
		return CertSMTPConfig{}
	}
	return cfg
}

// ---------------------------------------------------------------------
// 退避策略（Hard Rule：有界，禁止无限重试阻塞任务队列）
// ---------------------------------------------------------------------

// defaultCertAlertBackoff 固定退避序列：首次发送 + 序列内重试 =
// 最多 5 次尝试，退避累计 7s，之后停止重试、落失败记录。
var defaultCertAlertBackoff = []time.Duration{
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
}

// ---------------------------------------------------------------------
// 通道发送缝（webhook 复用 channel.CertWebhookSender；email 复用
// channel.EmailSender——按事件接收人动态构造）
// ---------------------------------------------------------------------

type certWebhookSender interface {
	Send(ctx context.Context, webhookURL string, payload *channel.CertWebhookPayload) error
}

type certEmailSender interface {
	SendCertEmail(ctx context.Context, msg *channel.Message, to []string) error
}

// certEmailChannel 邮件通道适配器：复用既有 channel.EmailSender。
type certEmailChannel struct {
	cfg CertSMTPConfig
}

func (c *certEmailChannel) SendCertEmail(ctx context.Context, msg *channel.Message, to []string) error {
	sender := channel.NewEmailSender(c.cfg.Host, c.cfg.Port, c.cfg.User, c.cfg.Pass, c.cfg.From, to)
	return sender.Send(ctx, msg)
}

// ---------------------------------------------------------------------
// 路由判定（纯函数，5.10 经 event.VerifyWindow 控制窗口关闭后的恢复）
// ---------------------------------------------------------------------

// CertAlertRoute change_linked 路由判定结果。
type CertAlertRoute struct {
	Via              string   // channel.CertRouteRegular | channel.CertRouteVerifyWindow
	WebhookURLs      []string // 本路由 webhook 接收人
	EmailGroup       []string // 本路由邮件接收人
	ChangeLinkedMark bool     // enabled=false 复用常规通道时的变更关联标记
	Dedicated        bool     // true=verifyWindowRoute 专用通道
}

// RouteCertAlert verifyWindowRoute 路由判定（任务 4.3 入参化纯函数）：
//   - category != change_linked → 常规通道（四类其余三类、ops、test 均常规路由）
//   - change_linked 且窗口已关闭（VerifyWindow.Active=false，由 5.10 控制）
//     → 恢复常规路由（不带变更关联标记语义）
//   - change_linked 且窗口开启 且 verifyWindowRoute.enabled=true → 专用通道
//   - change_linked 且窗口开启 且 enabled=false → 常规通道 + ChangeLinkedMark
//
// VerifyWindow 为 nil 视同窗口开启（change_linked 事件仅在验证窗口内产生）。
func RouteCertAlert(evt certservice.CertAlertEvent, cfg certdomain.AlertConfig) CertAlertRoute {
	route := CertAlertRoute{
		Via:         channel.CertRouteRegular,
		WebhookURLs: cfg.WebhookURLs,
		EmailGroup:  cfg.EmailGroup,
	}
	if evt.Category != certservice.AlertCategoryChangeLinked {
		return route
	}
	if evt.VerifyWindow != nil && !evt.VerifyWindow.Active {
		return route // 窗口关闭：恢复常规路由
	}
	vwr := cfg.VerifyWindowRoute
	if vwr == nil || !vwr.Enabled {
		route.ChangeLinkedMark = true // 复用常规通道 + 变更关联标记
		return route
	}
	route.Via = channel.CertRouteVerifyWindow
	route.Dedicated = true
	route.WebhookURLs = vwr.WebhookURLs
	route.EmailGroup = vwr.EmailGroup
	return route
}

// certAlertSeverity 类别 → 告警级别（邮件/webhook/投递记录共用）。
func certAlertSeverity(evt certservice.CertAlertEvent) domain.Severity {
	switch evt.Category {
	case certservice.AlertCategoryExpiry:
		if evt.Level == certdomain.ExpiryAlertL7 || evt.Level == certdomain.ExpiryAlertExpired {
			return domain.SeverityCritical
		}
		return domain.SeverityWarning
	case certservice.AlertCategoryRollbackFailed:
		return domain.SeverityCritical
	case certservice.AlertCategoryTest:
		return domain.SeverityInfo
	default: // tls_diff / change_linked / ops
		return domain.SeverityWarning
	}
}

// ---------------------------------------------------------------------
// CertAlertPublisher：certservice.CertAlertPublisher 实现（4.2 缝的消费方）
// ---------------------------------------------------------------------

// CertAlertPublisher 证书域告警发布器：按 cert AlertConfig 触达 webhook+email，
// verifyWindowRoute 分流，有界退避重试，投递记录持久化（ecam_alert_event，
// 终态直写——不经 pending 队列，避免既有 ProcessPendingEvents 拾取）。
type CertAlertPublisher struct {
	alertCfg  certdomain.AlertConfigRepository
	dao       dao.AlertDAO
	logger    *elog.Component
	smtp      CertSMTPConfig
	smtpReady bool
	webhook   certWebhookSender
	email     certEmailSender
	backoff   []time.Duration
	sleep     func(ctx context.Context, d time.Duration) error
	now       func() time.Time
}

// NewCertAlertPublisher 创建证书域告警发布器。
//
// smtp.Host 为空 → 邮件通道停用 + 启动警告（Hard：缺省警告而非失败，
// webhook 通道不受影响）。dao 用于投递记录（终态留存供补发/查询）。
func NewCertAlertPublisher(
	alertCfg certdomain.AlertConfigRepository,
	dao dao.AlertDAO,
	smtp CertSMTPConfig,
	logger *elog.Component,
) *CertAlertPublisher {
	if logger == nil {
		logger = elog.DefaultLogger
	}
	if smtp.Port == 0 && smtp.Host != "" {
		smtp.Port = defaultCertSMTPPort
	}
	smtpReady := smtp.Host != ""
	if !smtpReady {
		logger.Warn("证书告警邮件通道停用：alert.cert_smtp 未配置（webhook 通道不受影响）")
	}
	return &CertAlertPublisher{
		alertCfg:  alertCfg,
		dao:       dao,
		logger:    logger,
		smtp:      smtp,
		smtpReady: smtpReady,
		webhook:   channel.NewCertWebhookSender(),
		email:     &certEmailChannel{cfg: smtp},
		backoff:   defaultCertAlertBackoff,
		sleep:     sleepWithContext,
		now:       time.Now,
	}
}

// PublishAlert 发布单条证书域告警（certservice.CertAlertPublisher 接口实现）。
//
// 返回语义（与 4.2 at-least-once 约定一致）：
//   - nil：送达成功；或"无可投递接收人/邮件通道未配置"的配置态（事件已留存
//     failed 记录，不作为瞬时错误触发上轮重发）
//   - error：有接收人但最终投递失败（退避耗尽）——调用方不落去重状态，下轮重发
func (p *CertAlertPublisher) PublishAlert(ctx context.Context, event certservice.CertAlertEvent) error {
	if event.Category == "" {
		return fmt.Errorf("cert alert category is required")
	}
	cfg, err := p.alertCfg.Get(ctx)
	if err != nil {
		return fmt.Errorf("cert alert: get alert config: %w", err)
	}
	route := RouteCertAlert(event, cfg)

	// 无任何接收人：留存 failed 记录（事件不丢失，供配置后补发/查询），非瞬时错误。
	if len(route.WebhookURLs) == 0 && len(route.EmailGroup) == 0 {
		p.recordDelivery(ctx, event, route, certDelivery{
			status:     domain.EventStatusFailed,
			attempts:   0,
			reason:     "no alert receivers configured",
			emailState: emailStateNotAttempted,
		})
		p.logger.Warn("证书告警无接收人配置，事件已留存",
			elog.String("category", string(event.Category)),
			elog.String("title", event.Title))
		return nil
	}

	severity := certAlertSeverity(event)
	payload := channel.BuildCertWebhookPayload(event, severity, route.Via, route.ChangeLinkedMark)
	emailMsg := &channel.Message{Title: event.Title, Content: certEmailBody(payload), Severity: severity}

	emailNeeded := len(route.EmailGroup) > 0 && p.smtpReady
	emailOnly := len(route.WebhookURLs) == 0
	// 仅配置了邮件接收人但 SMTP 未配置：无可投递通道（配置态，记录后返回 nil）。
	if emailOnly && !emailNeeded {
		p.recordDelivery(ctx, event, route, certDelivery{
			status:     domain.EventStatusFailed,
			attempts:   0,
			reason:     "email receivers configured but smtp not configured",
			emailState: emailStateSkippedNoSMTP,
		})
		p.logger.Warn("证书告警仅有邮件接收人且 SMTP 未配置，事件已留存",
			elog.String("category", string(event.Category)))
		return nil
	}

	webhookDone := make([]bool, len(route.WebhookURLs))
	var emailDone bool
	maxAttempts := len(p.backoff) + 1
	var lastReasons []string

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var reasons []string
		for i, u := range route.WebhookURLs {
			if webhookDone[i] {
				continue // 已成功 URL 不重复投递
			}
			if err := p.webhook.Send(ctx, u, payload); err != nil {
				reasons = append(reasons, fmt.Sprintf("webhook[%d]: %v", i, err))
			} else {
				webhookDone[i] = true
			}
		}
		if emailNeeded && !emailDone {
			if err := p.email.SendCertEmail(ctx, emailMsg, route.EmailGroup); err != nil {
				reasons = append(reasons, fmt.Sprintf("email: %v", err))
			} else {
				emailDone = true
			}
		}

		if len(reasons) == 0 {
			// 全部目标送达（或 email 因 SMTP 未配置跳过且 webhook 已达）。
			emailState := emailStateSent
			if !emailNeeded && len(route.EmailGroup) > 0 {
				emailState = emailStateSkippedNoSMTP
			}
			p.recordDelivery(ctx, event, route, certDelivery{
				status:     domain.EventStatusSent,
				attempts:   attempt,
				emailState: emailState,
			})
			return nil
		}
		lastReasons = reasons
		p.logger.Warn("证书告警投递失败，准备退避重试",
			elog.String("category", string(event.Category)),
			elog.Int("attempt", attempt),
			elog.Int("max_attempts", maxAttempts),
			elog.String("reasons", strings.Join(reasons, "; ")))

		if attempt == maxAttempts {
			break
		}
		if err := p.sleep(ctx, p.backoff[attempt-1]); err != nil {
			lastReasons = append(lastReasons, fmt.Sprintf("backoff aborted: %v", err))
			break
		}
	}

	// 退避耗尽/中止：留存 failed 记录（含原因与尝试次数，供补发/查询），返回错误。
	reason := strings.Join(lastReasons, "; ")
	p.recordDelivery(ctx, event, route, certDelivery{
		status:     domain.EventStatusFailed,
		attempts:   maxAttempts,
		reason:     reason,
		emailState: emailEmailState(emailDone, emailNeeded, len(route.EmailGroup)),
	})
	return fmt.Errorf("cert alert delivery failed after %d attempts: %s", maxAttempts, reason)
}

// emailEmailState 汇总邮件通道终态。
func emailEmailState(done, needed bool, receivers int) certEmailState {
	switch {
	case receivers == 0:
		return emailStateNotConfigured
	case !needed:
		return emailStateSkippedNoSMTP
	case done:
		return emailStateSent
	default:
		return emailStateFailed
	}
}

// sleepWithContext 退避等待（ctx 取消即中断，防止长退避阻塞任务队列退出）。
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ---------------------------------------------------------------------
// 投递记录（ecam_alert_event 终态直写：status=sent/failed，不入 pending 队列）
// ---------------------------------------------------------------------

type certEmailState string

const (
	emailStateNotConfigured certEmailState = "not_configured"  // 无邮件接收人
	emailStateNotAttempted  certEmailState = "not_attempted"   // 有接收人但未走到投递
	emailStateSkippedNoSMTP certEmailState = "skipped_no_smtp" // SMTP 未配置跳过
	emailStateSent          certEmailState = "sent"
	emailStateFailed        certEmailState = "failed"
)

type certDelivery struct {
	status     domain.EventStatus
	attempts   int
	reason     string
	emailState certEmailState
}

// recordDelivery 写入投递记录；记录失败仅告警（不影响投递结果语义）。
// Content 为白名单键（不含 webhook URL——URL 可能内嵌 token）。
func (p *CertAlertPublisher) recordDelivery(
	ctx context.Context,
	event certservice.CertAlertEvent,
	route CertAlertRoute,
	d certDelivery,
) {
	if p.dao == nil {
		return
	}
	content := map[string]any{
		"category":    string(event.Category),
		"routed_via":  route.Via,
		"email_state": string(d.emailState),
		"attempts":    d.attempts,
	}
	if event.Fingerprint != "" {
		content["fingerprint"] = event.Fingerprint
	}
	if event.OrderID != "" {
		content["order_id"] = event.OrderID
	}
	if event.Domain != "" {
		content["domain"] = event.Domain
	}
	if d.reason != "" {
		content["reason"] = d.reason
	}
	rec := domain.AlertEvent{
		Type:     domain.AlertType("cert_" + string(event.Category)),
		Severity: certAlertSeverity(event),
		Title:    event.Title,
		Content:  content,
		Source:   "cert_alert:" + string(event.Category),
		Status:   d.status,
	}
	if d.status == domain.EventStatusSent {
		now := p.now()
		rec.SentAt = &now
	}
	if _, err := p.dao.CreateEvent(ctx, rec); err != nil {
		p.logger.Error("证书告警投递记录写入失败",
			elog.String("category", string(event.Category)),
			elog.FieldErr(err))
	}
}

// certEmailBody 邮件正文：由白名单 payload 渲染（与 webhook 同源，字段集受
// CertWebhookPayload 白名单约束——不含私钥/凭证类材料）。
func certEmailBody(pl *channel.CertWebhookPayload) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("类别: %s\n级别: %s\n", pl.Category, pl.Severity))
	b.WriteString(fmt.Sprintf("标题: %s\n", pl.Title))
	if pl.Fingerprint != "" {
		b.WriteString(fmt.Sprintf("证书指纹: %s\n", pl.Fingerprint))
	}
	if pl.Level != "" {
		b.WriteString(fmt.Sprintf("到期分级: %s (剩余 %d 天, 截止 %s)\n", pl.Level, pl.DaysLeft, pl.NotAfter))
	}
	if pl.Domain != "" {
		b.WriteString(fmt.Sprintf("域名: %s\n", pl.Domain))
	}
	if pl.OrderID != "" {
		b.WriteString(fmt.Sprintf("变更单: %s\n", pl.OrderID))
	}
	if len(pl.SANs) > 0 {
		b.WriteString(fmt.Sprintf("SAN 摘要: %s\n", strings.Join(pl.SANs, ", ")))
	}
	if pl.Detail != "" {
		b.WriteString(fmt.Sprintf("详情: %s\n", pl.Detail))
	}
	if pl.ExpectedFingerprint != "" {
		b.WriteString(fmt.Sprintf("预期指纹: %s\n", pl.ExpectedFingerprint))
		b.WriteString(fmt.Sprintf("窗口达标计数: %d\n", pl.PassCount))
	}
	if pl.ChangeLinked {
		b.WriteString("变更关联: 是（验证窗口路由未启用，经常规通道触达）\n")
	}
	b.WriteString(fmt.Sprintf("路由: %s\n事件时间: %s\n", pl.RoutedVia, pl.At))
	return b.String()
}

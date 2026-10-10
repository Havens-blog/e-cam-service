package alertpub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	certdomain "github.com/Havens-blog/e-cam-service/internal/cert/domain"
	certservice "github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------
// 测试替身
// ---------------------------------------------------------------------

// stubCertConfigRepo certdomain.AlertConfigRepository 桩（内存配置）。
type stubCertConfigRepo struct {
	mu  sync.Mutex
	cfg certdomain.AlertConfig
}

func (r *stubCertConfigRepo) Get(context.Context) (certdomain.AlertConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg, nil
}

func (r *stubCertConfigRepo) Save(_ context.Context, cfg *certdomain.AlertConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg = *cfg
	return nil
}

// stubRecorder DeliveryRecorder 桩：捕获投递记录。
type stubRecorder struct {
	mu   sync.Mutex
	recs []DeliveryRecord
}

func (d *stubRecorder) Record(_ context.Context, rec DeliveryRecord) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.recs = append(d.recs, rec)
	return nil
}

func (d *stubRecorder) records() []DeliveryRecord {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]DeliveryRecord, len(d.recs))
	copy(out, d.recs)
	return out
}

// capturedEmail stub EmailSink 捕获的单封邮件。
type capturedEmail struct {
	Title    string
	Body     string
	Severity string
	To       []string
}

// stubEmailSink EmailSink 桩：捕获发信调用（替代真实 SMTP——SMTP 传输属
// alert 通用基建，由组合根 adapter 覆盖；本包仅验证发布器的邮件分发决策与
// 正文渲染）。failErr 非 nil 时模拟发信失败。
type stubEmailSink struct {
	mu      sync.Mutex
	mails   []capturedEmail
	failErr error
}

func (s *stubEmailSink) SendCertEmail(_ context.Context, title, body, severity string, to []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failErr != nil {
		return s.failErr
	}
	s.mails = append(s.mails, capturedEmail{Title: title, Body: body, Severity: severity, To: append([]string(nil), to...)})
	return nil
}

func (s *stubEmailSink) received() []capturedEmail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]capturedEmail(nil), s.mails...)
}

// hookStats mock HTTP server 统计（webhook 成功/失败/限流场景共用）。
type hookStats struct {
	mu     sync.Mutex
	count  int
	bodies []map[string]any
}

func (s *hookStats) snapshot() (int, []map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count, append([]map[string]any(nil), s.bodies...)
}

// newHookServer mock webhook server：前 failFirstN 次返回 failStatus，其后 200。
func newHookServer(t *testing.T, failFirstN int, failStatus int) (*httptest.Server, *hookStats) {
	t.Helper()
	stats := &hookStats{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stats.mu.Lock()
		stats.count++
		n := stats.count
		stats.mu.Unlock()
		if n <= failFirstN {
			w.WriteHeader(failStatus)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		stats.mu.Lock()
		stats.bodies = append(stats.bodies, body)
		stats.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, stats
}

// ---------------------------------------------------------------------
// harness
// ---------------------------------------------------------------------

type certPublisherHarness struct {
	*CertAlertPublisher
	cfgRepo  *stubCertConfigRepo
	recorder *stubRecorder
	email    *stubEmailSink

	sleptMu sync.Mutex
	slept   []time.Duration
}

// newCertPublisherHarness 构造发布器测试夹具。emailOn=false 时 email sink 注入
// nil（模拟 SMTP 未配置：邮件通道停用，webhook 不受影响）。
func newCertPublisherHarness(t *testing.T, cfg certdomain.AlertConfig, emailOn bool) *certPublisherHarness {
	t.Helper()
	h := &certPublisherHarness{
		cfgRepo:  &stubCertConfigRepo{cfg: cfg},
		recorder: &stubRecorder{},
		email:    &stubEmailSink{},
	}
	var sink EmailSink
	if emailOn {
		sink = h.email
	}
	p := NewCertAlertPublisher(h.cfgRepo, h.recorder, sink, nil)
	p.sleep = func(_ context.Context, d time.Duration) error {
		h.sleptMu.Lock()
		h.slept = append(h.slept, d)
		h.sleptMu.Unlock()
		return nil
	}
	h.CertAlertPublisher = p
	return h
}

func (h *certPublisherHarness) sleepLog() []time.Duration {
	h.sleptMu.Lock()
	defer h.sleptMu.Unlock()
	return append([]time.Duration(nil), h.slept...)
}

func webhookCfg(urls ...string) certdomain.AlertConfig {
	return certdomain.AlertConfig{ID: certdomain.AlertConfigID, WebhookURLs: urls, EmailGroup: []string{}}
}

func expiryEvent() certservice.CertAlertEvent {
	return certservice.CertAlertEvent{
		Category:    certservice.AlertCategoryExpiry,
		Title:       "证书到期分级 L7：剩余 7 天",
		Fingerprint: strings.Repeat("ab", 32),
		SANs:        []string{"a.example.com"},
		Level:       certdomain.ExpiryAlertL7,
		DaysLeft:    7,
		NotAfter:    time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		At:          time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}
}

func changeLinkedEvent(active bool) certservice.CertAlertEvent {
	return certservice.CertAlertEvent{
		Category: certservice.AlertCategoryChangeLinked,
		Title:    "验证窗口变更关联差异",
		Domain:   "www.example.com",
		OrderID:  "order-1",
		At:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		VerifyWindow: &certservice.VerifyWindowContext{
			Active:              active,
			OrderID:             "order-1",
			ExpectedFingerprint: strings.Repeat("cd", 32),
			PassCount:           2,
		},
	}
}

// ---------------------------------------------------------------------
// 路由判定（AC2 两分支 + 窗口关闭回退）
// ---------------------------------------------------------------------

func TestRouteCertAlert(t *testing.T) {
	dedicated := &certdomain.VerifyWindowRoute{
		Enabled:     true,
		WebhookURLs: []string{"https://hooks.example.com/verify"},
		EmailGroup:  []string{"change@example.com"},
	}
	base := certdomain.AlertConfig{
		WebhookURLs: []string{"https://hooks.example.com/regular"},
		EmailGroup:  []string{"ops@example.com"},
	}

	t.Run("非 change_linked 常规路由", func(t *testing.T) {
		r := RouteCertAlert(expiryEvent(), base)
		assert.Equal(t, CertRouteRegular, r.Via)
		assert.False(t, r.Dedicated)
		assert.False(t, r.ChangeLinkedMark)
		assert.Equal(t, base.WebhookURLs, r.WebhookURLs)
	})

	t.Run("enabled=false 复用常规通道+标记", func(t *testing.T) {
		cfg := base
		cfg.VerifyWindowRoute = &certdomain.VerifyWindowRoute{Enabled: false}
		r := RouteCertAlert(changeLinkedEvent(true), cfg)
		assert.Equal(t, CertRouteRegular, r.Via)
		assert.False(t, r.Dedicated)
		assert.True(t, r.ChangeLinkedMark)
		assert.Equal(t, base.WebhookURLs, r.WebhookURLs)
	})

	t.Run("enabled=true 窗口开启走专用通道", func(t *testing.T) {
		cfg := base
		cfg.VerifyWindowRoute = dedicated
		r := RouteCertAlert(changeLinkedEvent(true), cfg)
		assert.Equal(t, CertRouteVerifyWindow, r.Via)
		assert.True(t, r.Dedicated)
		assert.Equal(t, dedicated.WebhookURLs, r.WebhookURLs)
		assert.Equal(t, dedicated.EmailGroup, r.EmailGroup)
	})

	t.Run("VerifyWindow 为 nil 视同窗口开启", func(t *testing.T) {
		evt := changeLinkedEvent(true)
		evt.VerifyWindow = nil
		cfg := base
		cfg.VerifyWindowRoute = dedicated
		r := RouteCertAlert(evt, cfg)
		assert.True(t, r.Dedicated)
	})

	t.Run("窗口关闭恢复常规路由（5.10 控制 Active=false）", func(t *testing.T) {
		cfg := base
		cfg.VerifyWindowRoute = dedicated
		r := RouteCertAlert(changeLinkedEvent(false), cfg)
		assert.Equal(t, CertRouteRegular, r.Via)
		assert.False(t, r.Dedicated)
		assert.False(t, r.ChangeLinkedMark)
		assert.Equal(t, base.WebhookURLs, r.WebhookURLs)
	})
}

// ---------------------------------------------------------------------
// webhook 投递（AC1：POST JSON 含 category/orderId/摘要）
// ---------------------------------------------------------------------

func TestPublishAlertWebhookSuccessAndRecord(t *testing.T) {
	srv, stats := newHookServer(t, 0, 0)
	h := newCertPublisherHarness(t, webhookCfg(srv.URL), false)

	require.NoError(t, h.PublishAlert(context.Background(), expiryEvent()))

	count, bodies := stats.snapshot()
	require.Equal(t, 1, count, "单接收人应恰好一次投递")
	body := bodies[0]
	assert.Equal(t, "expiry", body["category"])
	assert.Equal(t, strings.Repeat("ab", 32), body["fingerprint"])
	assert.Equal(t, "L7", body["level"])
	assert.Equal(t, float64(7), body["daysLeft"])
	assert.NotEmpty(t, body["title"], "AC：POST JSON 含 category/摘要")
	assert.Empty(t, h.sleepLog()) // 成功不退避

	records := h.recorder.records()
	require.Len(t, records, 1)
	rec := records[0]
	assert.Equal(t, "cert_expiry", rec.Type)
	assert.Equal(t, statusSent, rec.Status)
	require.NotNil(t, rec.SentAt)
	assert.Equal(t, "regular", rec.Content["routed_via"])
	assert.Equal(t, 1, rec.Content["attempts"])
}

func TestPublishAlertCategoryRequired(t *testing.T) {
	h := newCertPublisherHarness(t, webhookCfg("https://unused.example.com"), false)
	err := h.PublishAlert(context.Background(), certservice.CertAlertEvent{Title: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "category")
}

func TestPublishAlertNoReceivers(t *testing.T) {
	_, stats := newHookServer(t, 0, 0)
	h := newCertPublisherHarness(t, webhookCfg(), false) // 无接收人

	require.NoError(t, h.PublishAlert(context.Background(), expiryEvent()),
		"无接收人为配置态：留存记录返回 nil，不作为瞬时错误触发上轮重发")

	count, _ := stats.snapshot()
	assert.Zero(t, count)
	records := h.recorder.records()
	require.Len(t, records, 1)
	assert.Equal(t, statusFailed, records[0].Status)
	assert.Equal(t, "no alert receivers configured", records[0].Content["reason"])
}

// ---------------------------------------------------------------------
// 退避重试（AC3：有界序列；500/429 后恢复；耗尽不再无限重试）
// ---------------------------------------------------------------------

func TestPublishAlertBackoffRetryThenSuccess(t *testing.T) {
	// 前 2 次 429（限流），第 3 次 200。
	srv, stats := newHookServer(t, 2, http.StatusTooManyRequests)
	h := newCertPublisherHarness(t, webhookCfg(srv.URL), false)

	require.NoError(t, h.PublishAlert(context.Background(), expiryEvent()))

	count, _ := stats.snapshot()
	assert.Equal(t, 3, count)
	assert.Equal(t, []time.Duration{500 * time.Millisecond, 1 * time.Second},
		h.sleepLog(), "固定退避序列前两档")

	records := h.recorder.records()
	require.Len(t, records, 1)
	assert.Equal(t, statusSent, records[0].Status)
	assert.Equal(t, 3, records[0].Content["attempts"])
}

func TestPublishAlertBackoffExhaustion(t *testing.T) {
	srv, stats := newHookServer(t, 1<<30, http.StatusInternalServerError) // 恒 500
	h := newCertPublisherHarness(t, webhookCfg(srv.URL), false)

	err := h.PublishAlert(context.Background(), expiryEvent())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after 5 attempts")

	count, _ := stats.snapshot()
	assert.Equal(t, 5, count, "尝试次数有界：1+4 次退避重试后停止，不无限重试")
	assert.Len(t, h.sleepLog(), 4, "退避序列 4 档全部用尽")

	records := h.recorder.records()
	require.Len(t, records, 1)
	rec := records[0]
	assert.Equal(t, statusFailed, rec.Status)
	assert.Contains(t, rec.Content["reason"], "status 500")
	assert.Equal(t, 5, rec.Content["attempts"])
}

func TestPublishAlertBackoffAbortOnContextCancel(t *testing.T) {
	srv, stats := newHookServer(t, 1<<30, http.StatusInternalServerError)
	h := newCertPublisherHarness(t, webhookCfg(srv.URL), false)
	h.sleep = func(context.Context, time.Duration) error {
		return context.Canceled
	}

	err := h.PublishAlert(context.Background(), expiryEvent())
	require.Error(t, err)

	count, _ := stats.snapshot()
	assert.Equal(t, 1, count, "ctx 取消后不再继续重试")
	records := h.recorder.records()
	require.Len(t, records, 1)
	assert.Equal(t, statusFailed, records[0].Status)
	assert.Contains(t, records[0].Content["reason"], "backoff aborted")
}

func TestPublishAlertPartialWebhookRetriesOnlyFailedURL(t *testing.T) {
	badSrv, _ := newHookServer(t, 1<<30, http.StatusInternalServerError)
	okSrv, okStats := newHookServer(t, 0, 0)
	h := newCertPublisherHarness(t, webhookCfg(badSrv.URL, okSrv.URL), false)

	err := h.PublishAlert(context.Background(), expiryEvent())
	require.Error(t, err, "存在恒失败 URL：最终失败（at-least-once）")

	okCount, _ := okStats.snapshot()
	assert.Equal(t, 1, okCount, "已成功 URL 不重复投递，仅重试失败目标")
}

// ---------------------------------------------------------------------
// verifyWindowRoute 端到端（AC2 两分支 + 窗口关闭）
// ---------------------------------------------------------------------

func TestPublishAlertVerifyWindowDedicatedRoute(t *testing.T) {
	regSrv, regStats := newHookServer(t, 0, 0)
	dedSrv, dedStats := newHookServer(t, 0, 0)
	cfg := webhookCfg(regSrv.URL)
	cfg.VerifyWindowRoute = &certdomain.VerifyWindowRoute{
		Enabled:     true,
		WebhookURLs: []string{dedSrv.URL},
		EmailGroup:  []string{"change@example.com"},
	}
	h := newCertPublisherHarness(t, cfg, false)

	require.NoError(t, h.PublishAlert(context.Background(), changeLinkedEvent(true)))

	regCount, _ := regStats.snapshot()
	assert.Zero(t, regCount, "常规通道不得收到窗口内 change_linked 事件")
	dedCount, dedBodies := dedStats.snapshot()
	require.Equal(t, 1, dedCount)
	body := dedBodies[0]
	assert.Equal(t, "change_linked", body["category"])
	assert.Equal(t, "order-1", body["orderId"])
	assert.Equal(t, strings.Repeat("cd", 32), body["expectedFingerprint"])
	assert.Equal(t, float64(2), body["passCount"])
	assert.Equal(t, "verify_window", body["routedVia"])
	assert.Equal(t, false, body["changeLinked"])

	records := h.recorder.records()
	require.Len(t, records, 1)
	assert.Equal(t, "verify_window", records[0].Content["routed_via"])
}

func TestPublishAlertVerifyWindowDisabledMark(t *testing.T) {
	regSrv, regStats := newHookServer(t, 0, 0)
	cfg := webhookCfg(regSrv.URL)
	cfg.VerifyWindowRoute = &certdomain.VerifyWindowRoute{Enabled: false} // 未启用
	h := newCertPublisherHarness(t, cfg, false)

	require.NoError(t, h.PublishAlert(context.Background(), changeLinkedEvent(true)))

	count, bodies := regStats.snapshot()
	require.Equal(t, 1, count)
	assert.Equal(t, "regular", bodies[0]["routedVia"])
	assert.Equal(t, true, bodies[0]["changeLinked"], "enabled=false 复用常规通道但附变更关联标记")
}

func TestPublishAlertVerifyWindowClosedFallsBackToRegular(t *testing.T) {
	regSrv, regStats := newHookServer(t, 0, 0)
	dedSrv, dedStats := newHookServer(t, 0, 0)
	cfg := webhookCfg(regSrv.URL)
	cfg.VerifyWindowRoute = &certdomain.VerifyWindowRoute{
		Enabled:     true,
		WebhookURLs: []string{dedSrv.URL},
	}
	h := newCertPublisherHarness(t, cfg, false)

	require.NoError(t, h.PublishAlert(context.Background(), changeLinkedEvent(false)))

	regCount, _ := regStats.snapshot()
	assert.Equal(t, 1, regCount, "窗口关闭：change_linked 恢复常规通道")
	dedCount, _ := dedStats.snapshot()
	assert.Zero(t, dedCount, "窗口关闭：专用通道不得收到事件")
}

// ---------------------------------------------------------------------
// ops 运维处置类（AlertCategoryOps，不计入四类业务告警）
// ---------------------------------------------------------------------

func TestPublishAlertOpsCategory(t *testing.T) {
	srv, stats := newHookServer(t, 0, 0)
	h := newCertPublisherHarness(t, webhookCfg(srv.URL), false)

	evt := certservice.CertAlertEvent{
		Category: certservice.AlertCategoryOps,
		Title:    "孤儿云证书清理失败",
		Detail:   "cloud=aliyun cloudCertId=cert-123 reason=AccessDenied",
		At:       time.Now(),
	}
	require.NoError(t, h.PublishAlert(context.Background(), evt))

	_, bodies := stats.snapshot()
	require.Len(t, bodies, 1)
	assert.Equal(t, "ops", bodies[0]["category"], "运维处置类可标记为非业务四类")

	records := h.recorder.records()
	require.Len(t, records, 1)
	assert.Equal(t, "cert_ops", records[0].Type)
	assert.Equal(t, string(SeverityWarning), records[0].Severity)
}

// ---------------------------------------------------------------------
// 邮件通道（stub EmailSink；AC5）
// ---------------------------------------------------------------------

func TestPublishAlertEmailViaSink(t *testing.T) {
	cfg := certdomain.AlertConfig{
		ID:          certdomain.AlertConfigID,
		WebhookURLs: []string{},
		EmailGroup:  []string{"a@example.com", "b@example.com"},
	}
	h := newCertPublisherHarness(t, cfg, true)

	evt := certservice.CertAlertEvent{
		Category: certservice.AlertCategoryTLSDiff,
		Title:    "TLS 差异告警",
		Domain:   "www.example.com",
		Detail:   "online fingerprint differs from ledger",
		At:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}
	require.NoError(t, h.PublishAlert(context.Background(), evt))

	mails := h.email.received()
	require.Len(t, mails, 1)
	mail := mails[0]
	assert.Equal(t, []string{"a@example.com", "b@example.com"}, mail.To)
	assert.Equal(t, "TLS 差异告警", mail.Title)
	assert.Contains(t, mail.Body, "www.example.com")
	assert.Contains(t, mail.Body, "类别: tls_diff")
	assert.Equal(t, string(SeverityWarning), mail.Severity)

	records := h.recorder.records()
	require.Len(t, records, 1)
	assert.Equal(t, statusSent, records[0].Status)
	assert.Equal(t, "sent", records[0].Content["email_state"])
}

func TestPublishAlertEmailOnlyNoSMTPConfigured(t *testing.T) {
	cfg := certdomain.AlertConfig{
		ID:          certdomain.AlertConfigID,
		WebhookURLs: []string{},
		EmailGroup:  []string{"a@example.com"},
	}
	h := newCertPublisherHarness(t, cfg, false) // email sink nil：启动警告而非失败

	require.NoError(t, h.PublishAlert(context.Background(), expiryEvent()))

	records := h.recorder.records()
	require.Len(t, records, 1)
	rec := records[0]
	assert.Equal(t, statusFailed, rec.Status)
	assert.Contains(t, rec.Content["reason"], "smtp not configured")
	assert.Equal(t, "skipped_no_smtp", rec.Content["email_state"])
}

func TestPublishAlertWebhookOKEmailSkippedNoSMTP(t *testing.T) {
	srv, stats := newHookServer(t, 0, 0)
	cfg := webhookCfg(srv.URL)
	cfg.EmailGroup = []string{"a@example.com"}
	h := newCertPublisherHarness(t, cfg, false) // email sink nil：webhook 仍触达

	require.NoError(t, h.PublishAlert(context.Background(), expiryEvent()))

	count, _ := stats.snapshot()
	assert.Equal(t, 1, count)
	records := h.recorder.records()
	require.Len(t, records, 1)
	assert.Equal(t, statusSent, records[0].Status)
	assert.Equal(t, "skipped_no_smtp", records[0].Content["email_state"])
}

func TestPublishAlertWebhookFailEmailSentStillFails(t *testing.T) {
	// webhook 恒失败 + 邮件可送达：最终失败（at-least-once），但邮件终态=sent。
	srv, _ := newHookServer(t, 1<<30, http.StatusInternalServerError)
	cfg := webhookCfg(srv.URL)
	cfg.EmailGroup = []string{"a@example.com"}
	h := newCertPublisherHarness(t, cfg, true)

	err := h.PublishAlert(context.Background(), expiryEvent())
	require.Error(t, err)

	assert.Len(t, h.email.received(), 1, "邮件在首轮即送达，后续重试不重复发信")
	records := h.recorder.records()
	require.Len(t, records, 1)
	assert.Equal(t, statusFailed, records[0].Status)
	assert.Equal(t, "sent", records[0].Content["email_state"])
	assert.Contains(t, records[0].Content["reason"], "webhook[0]")
}

// ---------------------------------------------------------------------
// 级别映射
// ---------------------------------------------------------------------

func TestCertAlertSeverity(t *testing.T) {
	cases := []struct {
		category certservice.AlertCategory
		level    certdomain.ExpiryAlertLevel
		want     Severity
	}{
		{certservice.AlertCategoryExpiry, certdomain.ExpiryAlertL30, SeverityWarning},
		{certservice.AlertCategoryExpiry, certdomain.ExpiryAlertL14, SeverityWarning},
		{certservice.AlertCategoryExpiry, certdomain.ExpiryAlertL7, SeverityCritical},
		{certservice.AlertCategoryExpiry, certdomain.ExpiryAlertExpired, SeverityCritical},
		{certservice.AlertCategoryTLSDiff, "", SeverityWarning},
		{certservice.AlertCategoryChangeLinked, "", SeverityWarning},
		{certservice.AlertCategoryRollbackFailed, "", SeverityCritical},
		{certservice.AlertCategoryOps, "", SeverityWarning},
		{certservice.AlertCategoryTest, "", SeverityInfo},
	}
	for _, c := range cases {
		evt := certservice.CertAlertEvent{Category: c.category, Level: c.level}
		assert.Equal(t, c.want, certAlertSeverity(evt),
			"category=%s level=%s", c.category, c.level)
	}
}

// ---------------------------------------------------------------------
// 补充分支：邮件终态汇总 / sleep / recorder nil 安全
// ---------------------------------------------------------------------

func TestEmailEmailStateBranches(t *testing.T) {
	assert.Equal(t, emailStateNotConfigured, emailEmailState(false, false, 0))
	assert.Equal(t, emailStateSkippedNoSMTP, emailEmailState(false, false, 2))
	assert.Equal(t, emailStateSent, emailEmailState(true, true, 2))
	assert.Equal(t, emailStateFailed, emailEmailState(false, true, 2))
}

func TestSleepWithContext(t *testing.T) {
	// 正常到点。
	err := sleepWithContext(context.Background(), time.Millisecond)
	require.NoError(t, err)

	// ctx 先于计时器取消。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = sleepWithContext(ctx, time.Minute)
	require.ErrorIs(t, err, context.Canceled)
}

func TestNewCertAlertPublisherDefaults(t *testing.T) {
	// email sink 为 nil → 邮件通道停用（emailOn=false）。
	h := newCertPublisherHarness(t, webhookCfg(), false)
	assert.False(t, h.emailOn)

	h2 := newCertPublisherHarness(t, webhookCfg(), true)
	assert.True(t, h2.emailOn)

	// recorder 为 nil：recordDelivery 安全跳过（不影响投递主流程）。
	p := NewCertAlertPublisher(h.cfgRepo, nil, nil, nil)
	assert.NoError(t, p.PublishAlert(context.Background(), expiryEvent()))
}

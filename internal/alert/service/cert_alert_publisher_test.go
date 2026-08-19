package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/alert/channel"
	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
	certdomain "github.com/Havens-blog/e-cam-service/internal/cert/domain"
	certservice "github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------
// 测试替身
// ---------------------------------------------------------------------

var errNotImplemented = errors.New("stub: not implemented")

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

// stubAlertDAO dao.AlertDAO 桩：仅实现 CreateEvent（投递记录）。
type stubAlertDAO struct {
	mu     sync.Mutex
	events []domain.AlertEvent
}

func (d *stubAlertDAO) CreateEvent(_ context.Context, e domain.AlertEvent) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e.ID = int64(len(d.events) + 1)
	d.events = append(d.events, e)
	return e.ID, nil
}

func (d *stubAlertDAO) records() []domain.AlertEvent {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]domain.AlertEvent, len(d.events))
	copy(out, d.events)
	return out
}

func (d *stubAlertDAO) CreateRule(context.Context, domain.AlertRule) (int64, error) {
	return 0, errNotImplemented
}
func (d *stubAlertDAO) UpdateRule(context.Context, domain.AlertRule) error { return errNotImplemented }
func (d *stubAlertDAO) GetRuleByID(context.Context, int64) (domain.AlertRule, error) {
	return domain.AlertRule{}, errNotImplemented
}
func (d *stubAlertDAO) ListRules(context.Context, domain.AlertRuleFilter) ([]domain.AlertRule, int64, error) {
	return nil, 0, errNotImplemented
}
func (d *stubAlertDAO) DeleteRule(context.Context, int64) error { return errNotImplemented }
func (d *stubAlertDAO) UpdateEventStatus(context.Context, int64, domain.EventStatus) error {
	return errNotImplemented
}
func (d *stubAlertDAO) ListEvents(context.Context, domain.AlertEventFilter) ([]domain.AlertEvent, int64, error) {
	return nil, 0, errNotImplemented
}
func (d *stubAlertDAO) GetPendingEvents(context.Context, int) ([]domain.AlertEvent, error) {
	return nil, errNotImplemented
}
func (d *stubAlertDAO) IncrementRetry(context.Context, int64) error { return errNotImplemented }
func (d *stubAlertDAO) CreateChannel(context.Context, domain.NotificationChannel) (int64, error) {
	return 0, errNotImplemented
}
func (d *stubAlertDAO) UpdateChannel(context.Context, domain.NotificationChannel) error {
	return errNotImplemented
}
func (d *stubAlertDAO) GetChannelByID(context.Context, int64) (domain.NotificationChannel, error) {
	return domain.NotificationChannel{}, errNotImplemented
}
func (d *stubAlertDAO) ListChannels(context.Context, domain.ChannelFilter) ([]domain.NotificationChannel, int64, error) {
	return nil, 0, errNotImplemented
}
func (d *stubAlertDAO) DeleteChannel(context.Context, int64) error { return errNotImplemented }
func (d *stubAlertDAO) GetChannelsByIDs(context.Context, []int64) ([]domain.NotificationChannel, error) {
	return nil, errNotImplemented
}
func (d *stubAlertDAO) InitIndexes(context.Context) error { return errNotImplemented }

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

// fakeMail 一封被 mock SMTP 接收的邮件。
type fakeMail struct {
	From string
	To   []string
	Data string
}

// fakeSMTPServer 极简 mock SMTP：220/250/354 应答，捕获 MAIL/RCPT/DATA。
type fakeSMTPServer struct {
	listener net.Listener
	mu       sync.Mutex
	mails    []fakeMail
}

func startFakeSMTP(t *testing.T) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &fakeSMTPServer{listener: ln}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeSMTPServer) port() int { return s.listener.Addr().(*net.TCPAddr).Port }

func (s *fakeSMTPServer) mailsReceived() []fakeMail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]fakeMail(nil), s.mails...)
}

func (s *fakeSMTPServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *fakeSMTPServer) handle(conn net.Conn) {
	defer conn.Close()
	writeLine := func(line string) bool {
		_, err := conn.Write([]byte(line + "\r\n"))
		return err == nil
	}
	if !writeLine("220 fake.example.com ESMTP") {
		return
	}
	reader := bufio.NewReader(conn)
	var mail fakeMail
	inData := false
	for {
		raw, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line := strings.TrimRight(raw, "\r\n")
		if inData {
			if line == "." {
				inData = false
				s.mu.Lock()
				s.mails = append(s.mails, mail)
				s.mu.Unlock()
				writeLine("250 ok")
				continue
			}
			if strings.HasPrefix(line, "..") { // 还原 dot-stuffing
				line = line[1:]
			}
			mail.Data += line + "\n"
			continue
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO "), strings.HasPrefix(upper, "HELO "):
			writeLine("250 fake.example.com")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			mail.From = angleAddrValue(line[len("MAIL FROM:"):])
			writeLine("250 ok")
		case strings.HasPrefix(upper, "RCPT TO:"):
			mail.To = append(mail.To, angleAddrValue(line[len("RCPT TO:"):]))
			writeLine("250 ok")
		case upper == "DATA":
			inData = true
			writeLine("354 go")
		case upper == "QUIT":
			writeLine("221 bye")
			return
		default:
			writeLine("250 ok")
		}
	}
}

func angleAddrValue(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '<'); i >= 0 {
		if j := strings.IndexByte(s[i:], '>'); j > 0 {
			return s[i+1 : i+j]
		}
	}
	return s
}

// ---------------------------------------------------------------------
// harness
// ---------------------------------------------------------------------

type certPublisherHarness struct {
	*CertAlertPublisher
	cfgRepo *stubCertConfigRepo
	daoStub *stubAlertDAO

	sleptMu sync.Mutex
	slept   []time.Duration
}

func newCertPublisherHarness(t *testing.T, cfg certdomain.AlertConfig, smtp CertSMTPConfig) *certPublisherHarness {
	t.Helper()
	h := &certPublisherHarness{cfgRepo: &stubCertConfigRepo{cfg: cfg}, daoStub: &stubAlertDAO{}}
	p := NewCertAlertPublisher(h.cfgRepo, h.daoStub, smtp, nil)
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
		assert.Equal(t, channel.CertRouteRegular, r.Via)
		assert.False(t, r.Dedicated)
		assert.False(t, r.ChangeLinkedMark)
		assert.Equal(t, base.WebhookURLs, r.WebhookURLs)
	})

	t.Run("enabled=false 复用常规通道+标记", func(t *testing.T) {
		cfg := base
		cfg.VerifyWindowRoute = &certdomain.VerifyWindowRoute{Enabled: false}
		r := RouteCertAlert(changeLinkedEvent(true), cfg)
		assert.Equal(t, channel.CertRouteRegular, r.Via)
		assert.False(t, r.Dedicated)
		assert.True(t, r.ChangeLinkedMark)
		assert.Equal(t, base.WebhookURLs, r.WebhookURLs)
	})

	t.Run("enabled=true 窗口开启走专用通道", func(t *testing.T) {
		cfg := base
		cfg.VerifyWindowRoute = dedicated
		r := RouteCertAlert(changeLinkedEvent(true), cfg)
		assert.Equal(t, channel.CertRouteVerifyWindow, r.Via)
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
		assert.Equal(t, channel.CertRouteRegular, r.Via)
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
	h := newCertPublisherHarness(t, webhookCfg(srv.URL), CertSMTPConfig{})

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

	records := h.daoStub.records()
	require.Len(t, records, 1)
	rec := records[0]
	assert.Equal(t, domain.AlertType("cert_expiry"), rec.Type)
	assert.Equal(t, domain.EventStatusSent, rec.Status)
	require.NotNil(t, rec.SentAt)
	assert.Equal(t, "regular", rec.Content["routed_via"])
	assert.Equal(t, 1, rec.Content["attempts"])
}

func TestPublishAlertCategoryRequired(t *testing.T) {
	h := newCertPublisherHarness(t, webhookCfg("https://unused.example.com"), CertSMTPConfig{})
	err := h.PublishAlert(context.Background(), certservice.CertAlertEvent{Title: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "category")
}

func TestPublishAlertNoReceivers(t *testing.T) {
	_, stats := newHookServer(t, 0, 0)
	h := newCertPublisherHarness(t, webhookCfg(), CertSMTPConfig{}) // 无接收人

	require.NoError(t, h.PublishAlert(context.Background(), expiryEvent()),
		"无接收人为配置态：留存记录返回 nil，不作为瞬时错误触发上轮重发")

	count, _ := stats.snapshot()
	assert.Zero(t, count)
	records := h.daoStub.records()
	require.Len(t, records, 1)
	assert.Equal(t, domain.EventStatusFailed, records[0].Status)
	assert.Equal(t, "no alert receivers configured", records[0].Content["reason"])
}

// ---------------------------------------------------------------------
// 退避重试（AC3：有界序列；500/429 后恢复；耗尽不再无限重试）
// ---------------------------------------------------------------------

func TestPublishAlertBackoffRetryThenSuccess(t *testing.T) {
	// 前 2 次 429（限流），第 3 次 200。
	srv, stats := newHookServer(t, 2, http.StatusTooManyRequests)
	h := newCertPublisherHarness(t, webhookCfg(srv.URL), CertSMTPConfig{})

	require.NoError(t, h.PublishAlert(context.Background(), expiryEvent()))

	count, _ := stats.snapshot()
	assert.Equal(t, 3, count)
	assert.Equal(t, []time.Duration{500 * time.Millisecond, 1 * time.Second},
		h.sleepLog(), "固定退避序列前两档")

	records := h.daoStub.records()
	require.Len(t, records, 1)
	assert.Equal(t, domain.EventStatusSent, records[0].Status)
	assert.Equal(t, 3, records[0].Content["attempts"])
}

func TestPublishAlertBackoffExhaustion(t *testing.T) {
	srv, stats := newHookServer(t, 1<<30, http.StatusInternalServerError) // 恒 500
	h := newCertPublisherHarness(t, webhookCfg(srv.URL), CertSMTPConfig{})

	err := h.PublishAlert(context.Background(), expiryEvent())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after 5 attempts")

	count, _ := stats.snapshot()
	assert.Equal(t, 5, count, "尝试次数有界：1+4 次退避重试后停止，不无限重试")
	assert.Len(t, h.sleepLog(), 4, "退避序列 4 档全部用尽")

	records := h.daoStub.records()
	require.Len(t, records, 1)
	rec := records[0]
	assert.Equal(t, domain.EventStatusFailed, rec.Status)
	assert.Nil(t, rec.SentAt)
	assert.Contains(t, rec.Content["reason"], "status 500")
	assert.Equal(t, 5, rec.Content["attempts"])
}

func TestPublishAlertBackoffAbortOnContextCancel(t *testing.T) {
	srv, stats := newHookServer(t, 1<<30, http.StatusInternalServerError)
	h := newCertPublisherHarness(t, webhookCfg(srv.URL), CertSMTPConfig{})
	h.sleep = func(context.Context, time.Duration) error {
		return context.Canceled
	}

	err := h.PublishAlert(context.Background(), expiryEvent())
	require.Error(t, err)

	count, _ := stats.snapshot()
	assert.Equal(t, 1, count, "ctx 取消后不再继续重试")
	records := h.daoStub.records()
	require.Len(t, records, 1)
	assert.Equal(t, domain.EventStatusFailed, records[0].Status)
	assert.Contains(t, records[0].Content["reason"], "backoff aborted")
}

func TestPublishAlertPartialWebhookRetriesOnlyFailedURL(t *testing.T) {
	badSrv, _ := newHookServer(t, 1<<30, http.StatusInternalServerError)
	okSrv, okStats := newHookServer(t, 0, 0)
	h := newCertPublisherHarness(t, webhookCfg(badSrv.URL, okSrv.URL), CertSMTPConfig{})

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
	h := newCertPublisherHarness(t, cfg, CertSMTPConfig{})

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

	records := h.daoStub.records()
	require.Len(t, records, 1)
	assert.Equal(t, "verify_window", records[0].Content["routed_via"])
}

func TestPublishAlertVerifyWindowDisabledMark(t *testing.T) {
	regSrv, regStats := newHookServer(t, 0, 0)
	cfg := webhookCfg(regSrv.URL)
	cfg.VerifyWindowRoute = &certdomain.VerifyWindowRoute{Enabled: false} // 未启用
	h := newCertPublisherHarness(t, cfg, CertSMTPConfig{})

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
	h := newCertPublisherHarness(t, cfg, CertSMTPConfig{})

	require.NoError(t, h.PublishAlert(context.Background(), changeLinkedEvent(false)))

	regCount, _ := regStats.snapshot()
	dedCount, _ := dedStats.snapshot()
	assert.Equal(t, 1, regCount, "窗口关闭恢复常规路由")
	assert.Zero(t, dedCount)
}

// ---------------------------------------------------------------------
// 运维处置类（AC1：非业务四类标记）
// ---------------------------------------------------------------------

func TestPublishAlertOpsCategory(t *testing.T) {
	srv, stats := newHookServer(t, 0, 0)
	h := newCertPublisherHarness(t, webhookCfg(srv.URL), CertSMTPConfig{})

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

	records := h.daoStub.records()
	require.Len(t, records, 1)
	assert.Equal(t, domain.AlertType("cert_ops"), records[0].Type)
	assert.Equal(t, domain.SeverityWarning, records[0].Severity)
}

// ---------------------------------------------------------------------
// 邮件通道（mock SMTP；AC5）
// ---------------------------------------------------------------------

func TestPublishAlertEmailViaFakeSMTP(t *testing.T) {
	smtpSrv := startFakeSMTP(t)
	cfg := certdomain.AlertConfig{
		ID:          certdomain.AlertConfigID,
		WebhookURLs: []string{},
		EmailGroup:  []string{"a@example.com", "b@example.com"},
	}
	h := newCertPublisherHarness(t, cfg, CertSMTPConfig{Host: "127.0.0.1", Port: smtpSrv.port()})

	evt := certservice.CertAlertEvent{
		Category: certservice.AlertCategoryTLSDiff,
		Title:    "TLS 差异告警",
		Domain:   "www.example.com",
		Detail:   "online fingerprint differs from ledger",
		At:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}
	require.NoError(t, h.PublishAlert(context.Background(), evt))

	mails := smtpSrv.mailsReceived()
	require.Len(t, mails, 1)
	mail := mails[0]
	assert.Equal(t, []string{"a@example.com", "b@example.com"}, mail.To)
	assert.Contains(t, mail.Data, "TLS 差异告警")
	assert.Contains(t, mail.Data, "www.example.com")
	assert.Contains(t, mail.Data, "类别: tls_diff")

	records := h.daoStub.records()
	require.Len(t, records, 1)
	assert.Equal(t, domain.EventStatusSent, records[0].Status)
	assert.Equal(t, "sent", records[0].Content["email_state"])
}

func TestPublishAlertEmailOnlyNoSMTPConfigured(t *testing.T) {
	cfg := certdomain.AlertConfig{
		ID:          certdomain.AlertConfigID,
		WebhookURLs: []string{},
		EmailGroup:  []string{"a@example.com"},
	}
	h := newCertPublisherHarness(t, cfg, CertSMTPConfig{}) // SMTP 缺省：启动警告而非失败

	require.NoError(t, h.PublishAlert(context.Background(), expiryEvent()))

	records := h.daoStub.records()
	require.Len(t, records, 1)
	rec := records[0]
	assert.Equal(t, domain.EventStatusFailed, rec.Status)
	assert.Contains(t, rec.Content["reason"], "smtp not configured")
	assert.Equal(t, "skipped_no_smtp", rec.Content["email_state"])
}

func TestPublishAlertWebhookOKEmailSkippedNoSMTP(t *testing.T) {
	srv, stats := newHookServer(t, 0, 0)
	cfg := webhookCfg(srv.URL)
	cfg.EmailGroup = []string{"a@example.com"}
	h := newCertPublisherHarness(t, cfg, CertSMTPConfig{}) // SMTP 未配置：webhook 仍触达

	require.NoError(t, h.PublishAlert(context.Background(), expiryEvent()))

	count, _ := stats.snapshot()
	assert.Equal(t, 1, count)
	records := h.daoStub.records()
	require.Len(t, records, 1)
	assert.Equal(t, domain.EventStatusSent, records[0].Status)
	assert.Equal(t, "skipped_no_smtp", records[0].Content["email_state"])
}

// ---------------------------------------------------------------------
// 级别映射
// ---------------------------------------------------------------------

func TestCertAlertSeverity(t *testing.T) {
	cases := []struct {
		category certservice.AlertCategory
		level    certdomain.ExpiryAlertLevel
		want     domain.Severity
	}{
		{certservice.AlertCategoryExpiry, certdomain.ExpiryAlertL30, domain.SeverityWarning},
		{certservice.AlertCategoryExpiry, certdomain.ExpiryAlertL14, domain.SeverityWarning},
		{certservice.AlertCategoryExpiry, certdomain.ExpiryAlertL7, domain.SeverityCritical},
		{certservice.AlertCategoryExpiry, certdomain.ExpiryAlertExpired, domain.SeverityCritical},
		{certservice.AlertCategoryTLSDiff, "", domain.SeverityWarning},
		{certservice.AlertCategoryChangeLinked, "", domain.SeverityWarning},
		{certservice.AlertCategoryRollbackFailed, "", domain.SeverityCritical},
		{certservice.AlertCategoryOps, "", domain.SeverityWarning},
		{certservice.AlertCategoryTest, "", domain.SeverityInfo},
	}
	for _, c := range cases {
		evt := certservice.CertAlertEvent{Category: c.category, Level: c.level}
		assert.Equal(t, c.want, certAlertSeverity(evt),
			"category=%s level=%s", c.category, c.level)
	}
}

// ---------------------------------------------------------------------
// 补充分支：邮件终态汇总 / sleep / SMTP 配置装载
// ---------------------------------------------------------------------

func TestPublishAlertWebhookFailEmailSentStillFails(t *testing.T) {
	// webhook 恒失败 + 邮件可送达：最终失败（at-least-once），但邮件终态=sent。
	srv, _ := newHookServer(t, 1<<30, http.StatusInternalServerError)
	smtpSrv := startFakeSMTP(t)
	cfg := webhookCfg(srv.URL)
	cfg.EmailGroup = []string{"a@example.com"}
	h := newCertPublisherHarness(t, cfg, CertSMTPConfig{Host: "127.0.0.1", Port: smtpSrv.port()})

	err := h.PublishAlert(context.Background(), expiryEvent())
	require.Error(t, err)

	assert.Len(t, smtpSrv.mailsReceived(), 1, "邮件在首轮即送达，后续重试不重复发信")
	records := h.daoStub.records()
	require.Len(t, records, 1)
	assert.Equal(t, domain.EventStatusFailed, records[0].Status)
	assert.Equal(t, "sent", records[0].Content["email_state"])
	assert.Contains(t, records[0].Content["reason"], "webhook[0]")
}

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

func TestLoadCertSMTPConfig(t *testing.T) {
	viper.Set("alert.cert_smtp", map[string]any{
		"host": "smtp.example.com", "port": 465, "user": "u", "pass": "p", "from": "cert@example.com",
	})
	defer viper.Set("alert.cert_smtp", nil)

	cfg := LoadCertSMTPConfig()
	assert.Equal(t, "smtp.example.com", cfg.Host)
	assert.Equal(t, 465, cfg.Port)
	assert.Equal(t, "cert@example.com", cfg.From)

	viper.Set("alert.cert_smtp", nil)
	assert.Equal(t, CertSMTPConfig{}, LoadCertSMTPConfig(), "键缺省返回零值（启动警告而非失败）")
}

func TestNewCertAlertPublisherDefaults(t *testing.T) {
	// SMTP host 空 → 邮件停用；有 host 无 port → 默认 25。
	h := newCertPublisherHarness(t, webhookCfg(), CertSMTPConfig{})
	assert.False(t, h.smtpReady)

	h2 := newCertPublisherHarness(t, webhookCfg(), CertSMTPConfig{Host: "smtp.example.com"})
	assert.True(t, h2.smtpReady)
	assert.Equal(t, defaultCertSMTPPort, h2.smtp.Port)

	// dao 为 nil：recordDelivery 安全跳过（不影响投递主流程）。
	p := NewCertAlertPublisher(h.cfgRepo, nil, CertSMTPConfig{}, nil)
	assert.NoError(t, p.PublishAlert(context.Background(), expiryEvent()))
}

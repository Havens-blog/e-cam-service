package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// settingsHarness 全局配置服务测试装置。
type settingsHarness struct {
	svc       SettingsService
	alertCfg  *certtest.FakeAlertConfigRepo
	exempts   *certtest.FakeExemptionRepo
	publisher *InMemoryAlertPublisher
}

// newSettingsHarness 构造配置服务测试装置。
func newSettingsHarness(publisher CertAlertPublisher) *settingsHarness {
	h := &settingsHarness{
		alertCfg:  certtest.NewFakeAlertConfigRepo(),
		exempts:   certtest.NewFakeExemptionRepo(),
		publisher: NewInMemoryAlertPublisher(),
	}
	if publisher != nil {
		h.publisher = nil
		h.svc = NewSettingsService(h.alertCfg, h.exempts, publisher)
		return h
	}
	h.svc = NewSettingsService(h.alertCfg, h.exempts, h.publisher)
	return h
}

// validUpdate 合法更新入参基线。
func validUpdate() UpdateSettingsInput {
	return UpdateSettingsInput{
		WebhookURLs:            []string{"https://hooks.example.com/cert"},
		EmailGroup:             []string{"ops@example.com"},
		VerifyWindowRoute:      &domain.VerifyWindowRoute{Enabled: true, WebhookURLs: []string{"https://hooks.example.com/v"}, EmailGroup: []string{"v@example.com"}},
		WildcardProbeOverrides: map[string]string{"*.wild.example.com": "probe.wild.example.com"},
		Thresholds:             domain.DefaultThresholds(),
	}
}

// TestSettings_GetDefaults 未写入时返回 DEFAULT 视图。
func TestSettings_GetDefaults(t *testing.T) {
	h := newSettingsHarness(nil)
	view, err := h.svc.GetSettings(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{}, view.WebhookURLs)
	assert.False(t, view.ChannelConfirmed)
	assert.Equal(t, domain.DefaultThresholds(), view.Thresholds)
	assert.Empty(t, view.Exemptions)
	assert.NotNil(t, view.WildcardProbeOverrides, "override 空集归一为空 map 非 nil")
}

// TestSettings_UpdatePersists 合法更新全量落库（回读一致）。
func TestSettings_UpdatePersists(t *testing.T) {
	h := newSettingsHarness(nil)
	in := validUpdate()
	view, err := h.svc.UpdateSettings(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, in.WebhookURLs, view.WebhookURLs)
	assert.Equal(t, in.VerifyWindowRoute, view.VerifyWindowRoute)
	assert.Equal(t, in.WildcardProbeOverrides, view.WildcardProbeOverrides)

	persisted, err := h.alertCfg.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, in.Thresholds, persisted.Thresholds)
	assert.Equal(t, []string{"ops@example.com"}, persisted.EmailGroup)
}

// TestSettings_UpdateInvalidThresholdsRejected 越界整体拒绝：返回
// *ThresholdsInvalidError 且配置保持原值（无部分写入）。
func TestSettings_UpdateInvalidThresholdsRejected(t *testing.T) {
	h := newSettingsHarness(nil)
	// 先写入一份合法配置
	_, err := h.svc.UpdateSettings(context.Background(), validUpdate())
	require.NoError(t, err)

	in := validUpdate()
	in.Thresholds.PauseTimeoutHours = 23 // 越界（24~168）
	_, err = h.svc.UpdateSettings(context.Background(), in)
	var invalid *domain.ThresholdsInvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "pauseTimeoutHours", invalid.Field)

	// 配置保持原值
	persisted, err := h.alertCfg.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.DefaultThresholds(), persisted.Thresholds)
	assert.Equal(t, []string{"https://hooks.example.com/cert"}, persisted.WebhookURLs)
}

// TestSettings_UpdatePreservesChannelConfirmed 更新不改写 channelConfirmed。
func TestSettings_UpdatePreservesChannelConfirmed(t *testing.T) {
	h := newSettingsHarness(nil)
	cfg, err := h.alertCfg.Get(context.Background())
	require.NoError(t, err)
	cfg.ChannelConfirmed = true
	require.NoError(t, h.alertCfg.Save(context.Background(), &cfg))

	view, err := h.svc.UpdateSettings(context.Background(), validUpdate())
	require.NoError(t, err)
	assert.True(t, view.ChannelConfirmed, "PUT 面不触碰渠道确认状态")
}

// TestSettings_AddExemption 豁免 Upsert + 域 trim；空域拒绝。
func TestSettings_AddExemption(t *testing.T) {
	h := newSettingsHarness(nil)
	e, err := h.svc.AddExemption(context.Background(), ExemptionInput{
		Domain: " legacy.example.com ", Reason: "internal", Operator: "alice",
	})
	require.NoError(t, err)
	assert.Equal(t, "legacy.example.com", e.Domain)
	assert.Equal(t, "alice", e.Operator)

	list, err := h.exempts.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "legacy.example.com", list[0].Domain)

	_, err = h.svc.AddExemption(context.Background(), ExemptionInput{Domain: "   "})
	require.Error(t, err)
}

// TestSettings_RemoveExemption 移除豁免（幂等）。
func TestSettings_RemoveExemption(t *testing.T) {
	h := newSettingsHarness(nil)
	_, err := h.svc.AddExemption(context.Background(), ExemptionInput{Domain: "a.example.com"})
	require.NoError(t, err)
	require.NoError(t, h.svc.RemoveExemption(context.Background(), "a.example.com"))
	list, err := h.exempts.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, list)
	require.NoError(t, h.svc.RemoveExemption(context.Background(), "missing.example.com"))
}

// failingPublisher 可编程失败发布器。
type failingPublisher struct{ err error }

func (p *failingPublisher) PublishAlert(context.Context, CertAlertEvent) error { return p.err }

// TestSettings_SendTestAlert 三分支：无接收人 / 发布失败带原因 / 成功确认渠道。
func TestSettings_SendTestAlert(t *testing.T) {
	ctx := context.Background()

	// 1) 无接收人：sent=false + 原因，不发布
	h := newSettingsHarness(nil)
	res, err := h.svc.SendTestAlert(ctx, "alice")
	require.NoError(t, err)
	assert.False(t, res.Sent)
	assert.NotEmpty(t, res.Reason)
	assert.Empty(t, h.publisher.Events())

	// 2) 有接收人但通道失败：sent=false + 失败原因
	h2 := newSettingsHarness(&failingPublisher{err: errors.New("webhook timeout")})
	cfg, err := h2.alertCfg.Get(ctx)
	require.NoError(t, err)
	cfg.WebhookURLs = []string{"https://hooks.example.com/cert"}
	require.NoError(t, h2.alertCfg.Save(ctx, &cfg))
	res, err = h2.svc.SendTestAlert(ctx, "alice")
	require.NoError(t, err)
	assert.False(t, res.Sent)
	assert.Equal(t, "webhook timeout", res.Reason)
	persisted, _ := h2.alertCfg.Get(ctx)
	assert.False(t, persisted.ChannelConfirmed, "失败不确认渠道")

	// 3) 成功：sent=true + channelConfirmed 持久化
	h3 := newSettingsHarness(nil)
	cfg, err = h3.alertCfg.Get(ctx)
	require.NoError(t, err)
	cfg.EmailGroup = []string{"ops@example.com"}
	require.NoError(t, h3.alertCfg.Save(ctx, &cfg))
	res, err = h3.svc.SendTestAlert(ctx, "alice")
	require.NoError(t, err)
	assert.True(t, res.Sent)
	events := h3.publisher.Events()
	require.Len(t, events, 1)
	assert.Equal(t, AlertCategoryTest, events[0].Category)
	persisted, _ = h3.alertCfg.Get(ctx)
	assert.True(t, persisted.ChannelConfirmed)
}

// errAlertCfgRepo / errExemptionRepo 恒错仓储（错误分支覆盖）。
type errAlertCfgRepo struct{ err error }

func (r *errAlertCfgRepo) Get(context.Context) (domain.AlertConfig, error) {
	return domain.AlertConfig{}, r.err
}
func (r *errAlertCfgRepo) Save(context.Context, *domain.AlertConfig) error { return r.err }

type errExemptionRepo struct{ err error }

func (r *errExemptionRepo) Upsert(context.Context, *domain.Exemption) error { return r.err }
func (r *errExemptionRepo) List(context.Context) ([]domain.Exemption, error) {
	return nil, r.err
}
func (r *errExemptionRepo) DeleteByDomain(context.Context, string) error { return r.err }

// TestSettings_RepoErrorPropagation 仓储错误向上传播（含 message 前缀）。
func TestSettings_RepoErrorPropagation(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")

	// GetSettings：配置读取失败 / 豁免读取失败
	svc := NewSettingsService(&errAlertCfgRepo{boom}, certtest.NewFakeExemptionRepo(), nil)
	_, err := svc.GetSettings(ctx)
	require.ErrorContains(t, err, "boom")
	svc = NewSettingsService(certtest.NewFakeAlertConfigRepo(), &errExemptionRepo{boom}, nil)
	_, err = svc.GetSettings(ctx)
	require.ErrorContains(t, err, "boom")

	// UpdateSettings：校验通过后的读取/保存失败
	svc = NewSettingsService(&errAlertCfgRepo{boom}, certtest.NewFakeExemptionRepo(), nil)
	_, err = svc.UpdateSettings(ctx, validUpdate())
	require.ErrorContains(t, err, "boom")

	// AddExemption：写库失败；RemoveExemption：删除失败
	svc = NewSettingsService(certtest.NewFakeAlertConfigRepo(), &errExemptionRepo{boom}, nil)
	_, err = svc.AddExemption(ctx, ExemptionInput{Domain: "a.example.com"})
	require.ErrorContains(t, err, "boom")
	err = svc.RemoveExemption(ctx, "a.example.com")
	require.ErrorContains(t, err, "boom")
}

// TestSettings_SendTestAlertPersistError 发布成功但确认落库失败 → 错误向上传播。
func TestSettings_SendTestAlertPersistError(t *testing.T) {
	svc := NewSettingsService(&onceAlertCfgRepo{cfg: domain.AlertConfig{
		WebhookURLs: []string{"https://hooks.example.com"},
	}}, certtest.NewFakeExemptionRepo(), nil)
	_, err := svc.SendTestAlert(context.Background(), "alice")
	require.ErrorContains(t, err, "boom")
}

// onceAlertCfgRepo Get 成功一次��� Save 恒错（确认落库失败分支）。
type onceAlertCfgRepo struct{ cfg domain.AlertConfig }

func (r *onceAlertCfgRepo) Get(context.Context) (domain.AlertConfig, error) { return r.cfg, nil }
func (r *onceAlertCfgRepo) Save(context.Context, *domain.AlertConfig) error {
	return errors.New("boom")
}

// TestSettings_LoggingPublisherFallback publisher=nil 回退日志实现（构造分支）。
func TestSettings_LoggingPublisherFallback(t *testing.T) {
	svc := NewSettingsService(certtest.NewFakeAlertConfigRepo(), certtest.NewFakeExemptionRepo(), nil)
	require.NotNil(t, svc)
}

// TestValidateThresholds 纯函数口径：全字段越界逐一命中（首个返回）。
func TestValidateThresholds(t *testing.T) {
	require.NoError(t, domain.ValidateThresholds(domain.DefaultThresholds()))

	cases := []struct {
		name  string
		mut   func(*domain.Thresholds)
		field string
	}{
		{"scanFreshnessHours low", func(t *domain.Thresholds) { t.ScanFreshnessHours = 0 }, "scanFreshnessHours"},
		{"scanFreshnessHours high", func(t *domain.Thresholds) { t.ScanFreshnessHours = 73 }, "scanFreshnessHours"},
		{"verifyWindowHours low", func(t *domain.Thresholds) { t.VerifyWindowHours = 1 }, "verifyWindowHours"},
		{"verifyWindowHours high", func(t *domain.Thresholds) { t.VerifyWindowHours = 25 }, "verifyWindowHours"},
		{"rollbackProtectDays low", func(t *domain.Thresholds) { t.RollbackProtectDays = 6 }, "rollbackProtectDays"},
		{"rollbackProtectDays high", func(t *domain.Thresholds) { t.RollbackProtectDays = 15 }, "rollbackProtectDays"},
		{"verifyConfirmProbes high", func(t *domain.Thresholds) { t.VerifyConfirmProbes = 11 }, "verifyConfirmProbes"},
		{"verifyProbeIntervalMinutes low", func(t *domain.Thresholds) { t.VerifyProbeIntervalMinutes = 4 }, "verifyProbeIntervalMinutes"},
		{"verifyProbeIntervalMinutes high", func(t *domain.Thresholds) { t.VerifyProbeIntervalMinutes = 61 }, "verifyProbeIntervalMinutes"},
		{"pauseTimeoutHours low", func(t *domain.Thresholds) { t.PauseTimeoutHours = 23 }, "pauseTimeoutHours"},
		{"pauseTimeoutHours high", func(t *domain.Thresholds) { t.PauseTimeoutHours = 169 }, "pauseTimeoutHours"},
		{"recheckDelayMinutes high", func(t *domain.Thresholds) { t.RecheckDelayMinutes = 61 }, "recheckDelayMinutes"},
		{"itemHeartbeatTimeoutMinutes low", func(t *domain.Thresholds) { t.ItemHeartbeatTimeoutMinutes = 4 }, "itemHeartbeatTimeoutMinutes"},
		{"itemHeartbeatTimeoutMinutes high", func(t *domain.Thresholds) { t.ItemHeartbeatTimeoutMinutes = 181 }, "itemHeartbeatTimeoutMinutes"},
		{"scanTimeoutHours high", func(t *domain.Thresholds) { t.ScanTimeoutHours = 13 }, "scanTimeoutHours"},
		{"expiryLevels empty", func(t *domain.Thresholds) { t.ExpiryLevels = []int{} }, "expiryLevels"},
		{"expiryLevels too many", func(t *domain.Thresholds) { t.ExpiryLevels = []int{90, 80, 70, 60, 50, 40} }, "expiryLevels"},
		{"expiryLevels out of range", func(t *domain.Thresholds) { t.ExpiryLevels = []int{30, 91} }, "expiryLevels"},
		{"expiryLevels duplicated", func(t *domain.Thresholds) { t.ExpiryLevels = []int{30, 30} }, "expiryLevels"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th := domain.DefaultThresholds()
			tc.mut(&th)
			err := domain.ValidateThresholds(th)
			var invalid *domain.ThresholdsInvalidError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, tc.field, invalid.Field)
		})
	}
}

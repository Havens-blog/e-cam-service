package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------
// 测试基础设施：可注入时钟 + 内存告警发布器 + 台账假实现复用
// ---------------------------------------------------------------------

// inspBase 注入时钟基准（固定时点，避免挂钟抖动影响 ceil 边界断言）。
var inspBase = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

// inspectionHarness 巡检测试装置：now 可注入推进（模拟天级时序）。
type inspectionHarness struct {
	svc   InspectionService
	impl  *inspectionService
	certs *certtest.FakeCertificateRepo
	alert *fakeAlertCfgRepo
	pub   *InMemoryAlertPublisher
	now   time.Time
	seq   int
}

func newInspectionHarness(t *testing.T) *inspectionHarness {
	t.Helper()
	certs := certtest.NewFakeCertificateRepo()
	alert := &fakeAlertCfgRepo{cfg: domain.DefaultAlertConfig()}
	pub := NewInMemoryAlertPublisher()
	impl, ok := NewInspectionService(certs, alert, pub).(*inspectionService)
	require.True(t, ok, "NewInspectionService 应返回可注入时钟的实现")
	h := &inspectionHarness{
		svc:   impl,
		impl:  impl,
		certs: certs,
		alert: alert,
		pub:   pub,
		now:   inspBase,
	}
	impl.now = func() time.Time { return h.now }
	return h
}

// fp 生成唯一 64-hex 测试指纹（满足 schema ^[0-9a-f]{64}$ 口径）。
func (h *inspectionHarness) fp() string {
	h.seq++
	return fmt.Sprintf("%064x", h.seq)
}

// seed 写入一张台账证书并返回指纹；level 预置持久化去重状态（空串=none 默认）。
func (h *inspectionHarness) seed(t *testing.T, notAfter time.Time, sans []string, level domain.ExpiryAlertLevel) string {
	t.Helper()
	fp := h.fp()
	require.NoError(t, h.certs.Create(context.Background(), &domain.Certificate{
		Fingerprint:      fp,
		CommonName:       "cn-" + fp[:8],
		Sans:             sans,
		NotAfter:         notAfter,
		HostingStatus:    domain.HostingStatusComplete,
		ExpiryAlertLevel: level,
	}))
	return fp
}

// advance 推进注入时钟。
func (h *inspectionHarness) advance(d time.Duration) { h.now = h.now.Add(d) }

// persistedLevel 从仓储读回去重状态（Hard Rule：去重状态持久化于文档字段）。
func (h *inspectionHarness) persistedLevel(t *testing.T, fp string) domain.ExpiryAlertLevel {
	t.Helper()
	c, err := h.certs.GetByFingerprint(context.Background(), fp)
	require.NoError(t, err)
	return c.ExpiryAlertLevel
}

// expiryEvents 过滤到期分级类事件。
func (h *inspectionHarness) expiryEvents() []CertAlertEvent {
	var out []CertAlertEvent
	for _, e := range h.pub.Events() {
		if e.Category == AlertCategoryExpiry {
			out = append(out, e)
		}
	}
	return out
}

// ---------------------------------------------------------------------
// AC1：分级计算（daysLeft=ceil 口径、默认 30/14/7 三档、expired 边界）
// ---------------------------------------------------------------------

// TestComputeExpiryLevel_DefaultTiers 默认 [30,14,7] 降序匹配取最紧急命中级；
// daysLeft=ceil((notAfter-now)/24h)——部分天向上取整（29.5d→30 命中 L30）。
func TestComputeExpiryLevel_DefaultTiers(t *testing.T) {
	day := 24 * time.Hour
	cases := []struct {
		name   string
		offset time.Duration // notAfter = base + offset；<=0 为已过期
		want   domain.ExpiryAlertLevel
	}{
		{"31d 出全部区间 → none", 31 * day, domain.ExpiryAlertNone},
		{"30d 整 → L30", 30 * day, domain.ExpiryAlertL30},
		{"29.5d ceil 入 30 → L30", 29*day + 12*time.Hour, domain.ExpiryAlertL30},
		{"15d → L30", 15 * day, domain.ExpiryAlertL30},
		{"14d 整 → L14", 14 * day, domain.ExpiryAlertL14},
		{"13.5d ceil 入 14 → L14", 13*day + 12*time.Hour, domain.ExpiryAlertL14},
		{"8d → L14", 8 * day, domain.ExpiryAlertL14},
		{"7d 整 → L7", 7 * day, domain.ExpiryAlertL7},
		{"1ns ceil=1 → L7", time.Nanosecond, domain.ExpiryAlertL7},
		{"恰等于 now → expired", 0, domain.ExpiryAlertExpired},
		{"已过期 1s → expired", -time.Second, domain.ExpiryAlertExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ComputeExpiryLevel(inspBase.Add(tc.offset), inspBase, []int{30, 14, 7}))
		})
	}
}

// TestComputeExpiryLevel_CustomTiers 配置变化正确计级（AC5）：
// 档位标签按"降序阈值序号"映射（最缓档→L30、次档→L14、第 3 档及更紧急→L7）。
func TestComputeExpiryLevel_CustomTiers(t *testing.T) {
	day := 24 * time.Hour
	// [45,20]：45 档落 L30 槽、20 档落 L14 槽
	assert.Equal(t, domain.ExpiryAlertNone, ComputeExpiryLevel(inspBase.Add(46*day), inspBase, []int{45, 20}))
	assert.Equal(t, domain.ExpiryAlertL30, ComputeExpiryLevel(inspBase.Add(45*day), inspBase, []int{45, 20}))
	assert.Equal(t, domain.ExpiryAlertL30, ComputeExpiryLevel(inspBase.Add(25*day), inspBase, []int{45, 20}))
	assert.Equal(t, domain.ExpiryAlertL14, ComputeExpiryLevel(inspBase.Add(20*day), inspBase, []int{45, 20}))
	assert.Equal(t, domain.ExpiryAlertL14, ComputeExpiryLevel(inspBase.Add(10*day), inspBase, []int{45, 20}))
	assert.Equal(t, domain.ExpiryAlertExpired, ComputeExpiryLevel(inspBase.Add(-day), inspBase, []int{45, 20}))

	// 单档配置：唯一档即最缓档（L30 槽）
	assert.Equal(t, domain.ExpiryAlertNone, ComputeExpiryLevel(inspBase.Add(11*day), inspBase, []int{10}))
	assert.Equal(t, domain.ExpiryAlertL30, ComputeExpiryLevel(inspBase.Add(10*day), inspBase, []int{10}))
	assert.Equal(t, domain.ExpiryAlertL30, ComputeExpiryLevel(inspBase.Add(1*day), inspBase, []int{10}))

	// 五档配置（schema maxItems=5）超出三个标签槽位：第 3 档起折叠为 L7，
	// 映射单调——升级去重不变式保持
	five := []int{60, 45, 30, 14, 7}
	assert.Equal(t, domain.ExpiryAlertL14, ComputeExpiryLevel(inspBase.Add(31*day), inspBase, five)) // 命中 45 档
	assert.Equal(t, domain.ExpiryAlertL7, ComputeExpiryLevel(inspBase.Add(25*day), inspBase, five))  // 命中 30 档
	assert.Equal(t, domain.ExpiryAlertL7, ComputeExpiryLevel(inspBase.Add(5*day), inspBase, five))   // 命中 7 档

	// 空配置回退 schema.sql DEFAULT [30,14,7]
	assert.Equal(t, domain.ExpiryAlertL14, ComputeExpiryLevel(inspBase.Add(10*day), inspBase, nil))
}

// ---------------------------------------------------------------------
// AC2：去重状态机（升级触发 / 同级不重发 / 降级不触发 / 换证重置）
// ---------------------------------------------------------------------

// TestInspection_UpgradeChainFiresEachLevel 30→14→7→expired 逐级升级逐级触发，
// 每级告警一次且持久化状态同步更新。
func TestInspection_UpgradeChainFiresEachLevel(t *testing.T) {
	h := newInspectionHarness(t)
	day := 24 * time.Hour
	notAfter := inspBase.Add(25 * day)
	fp := h.seed(t, notAfter, []string{"a.example.com"}, domain.ExpiryAlertNone)

	steps := []struct {
		advance time.Duration
		level   domain.ExpiryAlertLevel
	}{
		{0, domain.ExpiryAlertL30},           // 剩 25d → L30
		{11 * day, domain.ExpiryAlertL14},    // 剩 14d → L14
		{7 * day, domain.ExpiryAlertL7},      // 剩 7d → L7
		{7 * day, domain.ExpiryAlertExpired}, // 到期 → expired
	}
	for _, step := range steps {
		h.advance(step.advance)
		sum, err := h.svc.InspectLedger(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, sum.Evaluated)
		assert.Equal(t, 1, sum.Triggered, "升级到 %s 应触发一次告警", step.level)
		assert.Equal(t, step.level, h.persistedLevel(t, fp), "去重状态应持久化为 %s", step.level)
	}

	events := h.expiryEvents()
	require.Len(t, events, 4)
	wantLevels := []domain.ExpiryAlertLevel{
		domain.ExpiryAlertL30, domain.ExpiryAlertL14, domain.ExpiryAlertL7, domain.ExpiryAlertExpired,
	}
	for i, ev := range events {
		assert.Equal(t, wantLevels[i], ev.Level, "第 %d 次事件级别", i)
	}
}

// TestInspection_SameLevelDailyRerunNoRealert 天级巡检重复执行：同级不重发
// （同一证书 30 天级仅告警一次）。
func TestInspection_SameLevelDailyRerunNoRealert(t *testing.T) {
	h := newInspectionHarness(t)
	day := 24 * time.Hour
	fp := h.seed(t, inspBase.Add(25*day), []string{"a.example.com"}, domain.ExpiryAlertNone)

	totalTriggered := 0
	for i := 0; i < 3; i++ {
		sum, err := h.svc.InspectLedger(context.Background())
		require.NoError(t, err)
		totalTriggered += sum.Triggered
	}
	assert.Equal(t, 1, totalTriggered, "同级重复巡检不得重发")
	assert.Len(t, h.expiryEvents(), 1)
	assert.Equal(t, domain.ExpiryAlertL30, h.persistedLevel(t, fp))
}

// TestInspection_DowngradeNotTriggered 降级（新计级较已触发级别更缓）不触发、
// 不重置、状态保持——去重状态仅"升级"与"换证出区间重置"两条写入路径。
func TestInspection_DowngradeNotTriggered(t *testing.T) {
	h := newInspectionHarness(t)
	day := 24 * time.Hour
	fp := h.seed(t, inspBase.Add(25*day), []string{"a.example.com"}, domain.ExpiryAlertL7)

	sum, err := h.svc.InspectLedger(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, sum.Triggered)
	assert.Equal(t, 0, sum.Reset)
	assert.Empty(t, h.pub.Events())
	assert.Equal(t, domain.ExpiryAlertL7, h.persistedLevel(t, fp), "降级不改写去重状态")
}

// TestInspection_RenewalResetThenRetrigger 换证重置：notAfter 回升出全部级别
// 区间 → 重置 none；随后时间推进重新跨入档位 → 重新触发。
func TestInspection_RenewalResetThenRetrigger(t *testing.T) {
	h := newInspectionHarness(t)
	day := 24 * time.Hour

	// 已触发 L7 的证书换证：新 notAfter 距今 200d（出 [30,14,7] 全部区间）
	fpRenewed := h.seed(t, inspBase.Add(200*day), []string{"a.example.com"}, domain.ExpiryAlertL7)
	// 对照：换证后仍在区间内（25d）不重置（按降级处理）
	fpInBand := h.seed(t, inspBase.Add(25*day), []string{"b.example.com"}, domain.ExpiryAlertL7)

	sum, err := h.svc.InspectLedger(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, sum.Reset, "仅出区间的证书重置")
	assert.Equal(t, 0, sum.Triggered)
	assert.Equal(t, domain.ExpiryAlertNone, h.persistedLevel(t, fpRenewed), "换证重置为 none")
	assert.Equal(t, domain.ExpiryAlertL7, h.persistedLevel(t, fpInBand), "区内不重置")
	assert.Empty(t, h.expiryEvents(), "重置不发告警")

	// 重新计级：推进至剩 30d → none→L30 升级再次触发；
	// 对照证（持久化 L7、notAfter=base+25d）同期已到期 → L7→expired 升级亦触发
	h.advance(170 * day)
	sum, err = h.svc.InspectLedger(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, sum.Triggered, "重置证重新计级触发 + 对照证 expired 升级触发")
	events := h.expiryEvents()
	require.Len(t, events, 2)
	// 台账假实现按 map 遍历返回（次序不定），断言与次序解耦。
	assert.ElementsMatch(t,
		[]domain.ExpiryAlertLevel{domain.ExpiryAlertL30, domain.ExpiryAlertExpired},
		[]domain.ExpiryAlertLevel{events[0].Level, events[1].Level})
	assert.Equal(t, domain.ExpiryAlertL30, h.persistedLevel(t, fpRenewed))
	assert.Equal(t, domain.ExpiryAlertExpired, h.persistedLevel(t, fpInBand))
}

// TestInspection_DedupStateSurvivesServiceRestart Hard Rule：去重状态持久化于
// expiryAlertLevel 字段——服务重启（全新实例+全新发布器）后同级巡检不重复告警。
func TestInspection_DedupStateSurvivesServiceRestart(t *testing.T) {
	h := newInspectionHarness(t)
	day := 24 * time.Hour
	fp := h.seed(t, inspBase.Add(25*day), []string{"a.example.com"}, domain.ExpiryAlertNone)

	sum, err := h.svc.InspectLedger(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, sum.Triggered)

	// 重启模拟：同一仓储、全新服务实例与内存发布器（进程内状态不参与去重）
	pub2 := NewInMemoryAlertPublisher()
	impl2, ok := NewInspectionService(h.certs, h.alert, pub2).(*inspectionService)
	require.True(t, ok)
	impl2.now = func() time.Time { return h.now }
	sum2, err := impl2.InspectLedger(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, sum2.Triggered, "重启后不得重复告警")
	assert.Empty(t, pub2.Events())
	assert.Equal(t, domain.ExpiryAlertL30, h.persistedLevel(t, fp))
}

// TestInspection_SkipsCertWithoutNotAfter notAfter 缺失（零值）的文档无法计级，
// 跳过不误报 expired。
func TestInspection_SkipsCertWithoutNotAfter(t *testing.T) {
	h := newInspectionHarness(t)
	fp := h.seed(t, time.Time{}, []string{"a.example.com"}, domain.ExpiryAlertNone)

	sum, err := h.svc.InspectLedger(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, sum.Evaluated)
	assert.Equal(t, 0, sum.Triggered)
	assert.Empty(t, h.pub.Events())
	assert.Equal(t, domain.ExpiryAlertNone, h.persistedLevel(t, fp))
}

// ---------------------------------------------------------------------
// AC4：到期分级事件聚合内容（指纹/SAN 摘要/级别/daysLeft/到期时间；不含私钥）
// ---------------------------------------------------------------------

// TestInspection_ExpiryEventAggregatedContent 事件按证书粒度聚合；渗透式自查：
// 事件内容不得携带私钥/密文片段。
func TestInspection_ExpiryEventAggregatedContent(t *testing.T) {
	h := newInspectionHarness(t)
	day := 24 * time.Hour
	notAfter := inspBase.Add(10 * day)
	fp := h.fp()
	require.NoError(t, h.certs.Create(context.Background(), &domain.Certificate{
		Fingerprint:   fp,
		CommonName:    "agg.example.com",
		Sans:          []string{"a.example.com", "b.example.com"},
		NotAfter:      notAfter,
		HostingStatus: domain.HostingStatusComplete,
		EncryptedPrivateKey: &domain.EncryptedSecret{
			Ciphertext: "LEAK-MARKER-CIPHERTEXT",
			KeyVersion: 1,
			Algo:       "AES-256-GCM",
		},
	}))

	sum, err := h.svc.InspectLedger(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, sum.Triggered)

	events := h.expiryEvents()
	require.Len(t, events, 1)
	ev := events[0]
	assert.Equal(t, AlertCategoryExpiry, ev.Category)
	assert.Equal(t, fp, ev.Fingerprint)
	assert.Equal(t, []string{"a.example.com", "b.example.com"}, ev.SANs)
	assert.Equal(t, domain.ExpiryAlertL14, ev.Level)
	assert.Equal(t, 10, ev.DaysLeft)
	assert.Equal(t, notAfter, ev.NotAfter)
	assert.False(t, ev.At.IsZero())

	// 渗透式自查口径：事件任何字段不得含私钥密文片段
	assert.NotContains(t, fmt.Sprintf("%+v", ev), "LEAK-MARKER")
}

// ---------------------------------------------------------------------
// AC5：thresholds 读取 AlertConfig 单文档，配置变化正确计级
// ---------------------------------------------------------------------

// TestInspection_ConfigThresholdsFromAlertConfig [45,20] 配置经 AlertConfig
// 仓储生效：45 档（L30 槽）→ 20 档（L14 槽）升级触发。
func TestInspection_ConfigThresholdsFromAlertConfig(t *testing.T) {
	h := newInspectionHarness(t)
	day := 24 * time.Hour
	cfg := domain.DefaultAlertConfig()
	cfg.Thresholds.ExpiryLevels = []int{45, 20}
	h.alert.cfg = cfg

	fpNear := h.seed(t, inspBase.Add(25*day), []string{"a.example.com"}, domain.ExpiryAlertNone) // 25≤45 命中 45 档
	fpFar := h.seed(t, inspBase.Add(50*day), []string{"b.example.com"}, domain.ExpiryAlertNone)  // 50>45 出区间

	sum, err := h.svc.InspectLedger(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, sum.Triggered)
	events := h.expiryEvents()
	require.Len(t, events, 1)
	assert.Equal(t, domain.ExpiryAlertL30, events[0].Level)
	assert.Equal(t, 25, events[0].DaysLeft)
	assert.Equal(t, domain.ExpiryAlertL30, h.persistedLevel(t, fpNear))

	// 推进 15d：fpNear 剩 10d（≤20 命中 20 档）→ 升级 L14；
	// fpFar 剩 35d（≤45 命中 45 档）→ none→L30 首次触发（同轮两证，断言按指纹过滤）
	h.advance(15 * day)
	sum, err = h.svc.InspectLedger(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, sum.Triggered)

	var nearEvents []CertAlertEvent
	for _, e := range h.expiryEvents() {
		if e.Fingerprint == fpNear {
			nearEvents = append(nearEvents, e)
		}
	}
	require.Len(t, nearEvents, 2)
	assert.Equal(t, domain.ExpiryAlertL30, nearEvents[0].Level)
	assert.Equal(t, 25, nearEvents[0].DaysLeft)
	assert.Equal(t, domain.ExpiryAlertL14, nearEvents[1].Level)
	assert.Equal(t, 10, nearEvents[1].DaysLeft)
	assert.Equal(t, domain.ExpiryAlertL14, h.persistedLevel(t, fpNear))
	assert.Equal(t, domain.ExpiryAlertL30, h.persistedLevel(t, fpFar))
}

// ---------------------------------------------------------------------
// 错误隔离与默认发布实现
// ---------------------------------------------------------------------

// failingCertRepo 包装台账假实现：指定指纹的 UpdateExpiryAlertLevel 注入失败。
type failingCertRepo struct {
	*certtest.FakeCertificateRepo
	failFingerprint string
}

func (f *failingCertRepo) UpdateExpiryAlertLevel(ctx context.Context, fp string, level domain.ExpiryAlertLevel) error {
	if fp == f.failFingerprint {
		return fmt.Errorf("injected persist failure for %s", fp)
	}
	return f.FakeCertificateRepo.UpdateExpiryAlertLevel(ctx, fp, level)
}

// TestInspection_PerCertFailureDoesNotAbortRound 单证状态更新失败不中断整轮：
// 事件已发布（at-least-once，状态未落 → 下轮重发），其余证书正常处理。
func TestInspection_PerCertFailureDoesNotAbortRound(t *testing.T) {
	h := newInspectionHarness(t)
	day := 24 * time.Hour
	fpBad := h.seed(t, inspBase.Add(25*day), []string{"a.example.com"}, domain.ExpiryAlertNone)
	fpOk := h.seed(t, inspBase.Add(10*day), []string{"b.example.com"}, domain.ExpiryAlertNone)

	repo := &failingCertRepo{FakeCertificateRepo: h.certs, failFingerprint: fpBad}
	impl, ok := NewInspectionService(repo, h.alert, h.pub).(*inspectionService)
	require.True(t, ok)
	impl.now = func() time.Time { return h.now }

	sum, err := impl.InspectLedger(context.Background())
	require.Error(t, err, "失败应聚合返回")
	assert.Contains(t, err.Error(), fpBad)
	assert.Equal(t, 1, sum.Triggered, "失败证不计触发，成功证正常")

	// 失败证：事件已发布（发布先于状态落库），状态保持 none → 下轮重发
	assert.Equal(t, domain.ExpiryAlertNone, h.persistedLevel(t, fpBad))
	// 成功证：不受失败证影响
	assert.Equal(t, domain.ExpiryAlertL14, h.persistedLevel(t, fpOk))
}

// TestAlertPublishers 四类 category 字面值 + 内存/日志默认发布实现。
func TestAlertPublishers(t *testing.T) {
	// Hard Rule：四类以 category 字段承载且互异（PRD Monitoring 口径）
	assert.Equal(t, AlertCategory("expiry"), AlertCategoryExpiry)
	assert.Equal(t, AlertCategory("tls_diff"), AlertCategoryTLSDiff)
	assert.Equal(t, AlertCategory("change_linked"), AlertCategoryChangeLinked)
	assert.Equal(t, AlertCategory("rollback_failed"), AlertCategoryRollbackFailed)

	// 内存实现：记录事件供测试/桥接消费
	mem := NewInMemoryAlertPublisher()
	require.NoError(t, mem.PublishAlert(context.Background(), CertAlertEvent{
		Category: AlertCategoryRollbackFailed,
		OrderID:  "order-1",
	}))
	require.NoError(t, mem.PublishAlert(context.Background(), CertAlertEvent{
		Category: AlertCategoryChangeLinked,
		OrderID:  "order-2",
		Domain:   "a.example.com",
	}))
	require.Len(t, mem.Events(), 2)
	assert.Equal(t, AlertCategoryRollbackFailed, mem.Events()[0].Category)

	// 日志实现：恒成功（4.3 通道落地前的默认发布路径）
	logp := NewLoggingAlertPublisher()
	require.NoError(t, logp.PublishAlert(context.Background(), CertAlertEvent{
		Category: AlertCategoryTLSDiff,
		Domain:   "x.example.com",
	}))
	require.NoError(t, logp.PublishAlert(context.Background(), CertAlertEvent{
		Category: AlertCategoryExpiry,
		Level:    domain.ExpiryAlertL7,
	}))
}

// TestNewInspectionService_NilPublisherDefaults publisher 传 nil 时回退日志实现
// （4.4 调度装配未接通道时不 panic）。
func TestNewInspectionService_NilPublisherDefaults(t *testing.T) {
	certs := certtest.NewFakeCertificateRepo()
	alert := &fakeAlertCfgRepo{cfg: domain.DefaultAlertConfig()}
	svc := NewInspectionService(certs, alert, nil)
	require.NotNil(t, svc)
}

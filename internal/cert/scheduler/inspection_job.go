// Package scheduler 组装证书域天级巡检流水线 Job（任务 4.4）。
//
// 注册挂入 internal/task 框架由 7.1 完成；本包仅提供可被调度器调用的 Job 入口
// 与幂等守卫。探测（4.1）与到期分级（4.2）内部已就绪，本包做编排：完整性复检
// → 到期分级计算与去重告警 → TLS 探测调度（台账全部 sans）→ 豁免过滤。
//
// Hard Rules（任务 4.4）：
//   - Job 内不得做云侧写操作：仅只读探测与告警发布——依赖面只有仓储读取、
//     ProbeService/InspectionService（均只读+事件发布）与 CertAlertPublisher，
//     不依赖任何 deployer/云 SDK/执行通道。
//   - 天级粒度：RunInspection 为单轮入口，不携带任何频率/分钟级参数；调度节奏
//     由 7.1 注册侧控制，验证窗口内提频探测仅由 5.10 调用 ProbeService 实现，
//     与本 Job 无关。
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
)

// 子步名（运行记录 Steps 序，即四步执行顺序）。
const (
	StepIntegrity  = "integrity" // 完整性复检（复跑 2.1 校验）
	StepExpiry     = "expiry"    // 到期分级计算与去重告警（4.2）
	StepProbe      = "probe"     // TLS 探测调度（台账全部 sans，4.1）
	StepExemption  = "exemption" // 豁免过滤（exempt 不告警，diff 发 tls_diff）
	integrityTitle = "证书完整性复检异常"
	diffTitleFmt   = "TLS 探测差异：%s"
)

// ErrInspectionInProgress 幂等守卫（单飞）：同进程内上一轮巡检仍在执行时，
// 并发触发立即拒绝——防止同一轮探测/告警重复执行。
var ErrInspectionInProgress = errors.New("cert inspection: previous round still in progress")

// ---------------------------------------------------------------------
// 运行记录（lastInspectionAt + 各子步成功率指标）
// ---------------------------------------------------------------------

// StepMetrics 巡检子步指标（随运行记录留存，供平台自身监控消费）。
// Extra 为步骤专属计数：integrity=anomalies；expiry=triggered/reset；
// probe=六态逐态计数；exemption=exemptProbed/diffAlerted/exemptUnreachable。
type StepMetrics struct {
	Name   string         // 子步名（Step* 常量）
	Ok     bool           // 步骤整体完成（false=步骤级错误，细节在聚合 error）
	Total  int            // 处理单元数：证（integrity/expiry）/探测结果（probe/exemption）
	Failed int            // 异常/失败单元数（integrity 异常证、probe unreachable 域）
	Extra  map[string]int // 步骤专属计数（见类型注释）
}

// SuccessRate 子步成功率：(Total-Failed)/Total；Total=0 视为 1（无可失败单元）。
func (m StepMetrics) SuccessRate() float64 {
	if m.Total <= 0 {
		return 1
	}
	return float64(m.Total-m.Failed) / float64(m.Total)
}

// InspectionRun 单轮巡检运行记录：At 即 lastInspectionAt 口径（供 dashboard 展示），
// Steps 为四步指标快照（供平台自身监控）。
type InspectionRun struct {
	At    time.Time     // 本轮巡检开始时点
	Steps []StepMetrics // 四步指标（执行顺序）
}

// Ok 整轮全部子步完成（任一步骤级错误即 false；单证/单域失败不改变步骤 Ok，
// 体现在 Failed 计数）。
func (r InspectionRun) Ok() bool {
	for _, s := range r.Steps {
		if !s.Ok {
			return false
		}
	}
	return true
}

// InspectionRunStore 巡检运行记录端口：lastInspectionAt 供 dashboard 展示
// （实现 service.LastInspectionSource），各子步指标供平台自身监控。
// 7.1 装配时以持久化实现替换默认内存实现即可，Job 侧零改动。
type InspectionRunStore interface {
	// RecordRun 记录单轮巡检运行（At 取本轮开始时点）。
	RecordRun(ctx context.Context, run InspectionRun) error
	// LastInspectionAt 返回最近巡检时点；ok=false 表示尚无巡检记录。
	LastInspectionAt(ctx context.Context) (at time.Time, ok bool, err error)
}

// MemoryInspectionRunStore 默认进程内实现：仅保留最近一轮（lastInspectionAt 口径）。
// 进程重启后无记录（巡检为天级任务，7.1 装配可换持久化实现弥合）。
type MemoryInspectionRunStore struct {
	mu     sync.Mutex
	latest *InspectionRun
}

// 编译期断言：默认实现即 dashboard 的最近巡检时点来源（4.5 LastInspectionSource）。
var _ service.LastInspectionSource = (*MemoryInspectionRunStore)(nil)

// NewMemoryInspectionRunStore 创建内存运行记录存储。
func NewMemoryInspectionRunStore() *MemoryInspectionRunStore {
	return &MemoryInspectionRunStore{}
}

// RecordRun 记录运行副本（最近一轮覆盖前一轮）。
func (s *MemoryInspectionRunStore) RecordRun(_ context.Context, run InspectionRun) error {
	stored := run
	stored.Steps = append([]StepMetrics(nil), run.Steps...)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = &stored
	return nil
}

// LastInspectionAt 返回最近一轮开始时点；无记录 ok=false。
func (s *MemoryInspectionRunStore) LastInspectionAt(context.Context) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest == nil {
		return time.Time{}, false, nil
	}
	return s.latest.At, true, nil
}

// ---------------------------------------------------------------------
// InspectionJob：天级巡检流水线入口
// ---------------------------------------------------------------------

// InspectionJob 天级巡检流水线：顺序执行四步（完整性复检 → 到期分级去重告警 →
// TLS 探测 → 豁免过滤）。单证/单域失败不中断整轮（错误聚合返回）；同日重复执行
// 不产生重复到期告警（依赖 4.2 expiryAlertLevel 升级去重）。
type InspectionJob struct {
	certs     domain.CertificateRepository // 台账读取（完整性复检）
	exempts   domain.ExemptionRepository   // 豁免清单读取（豁免过滤）
	expiry    service.InspectionService    // 4.2 到期分级去重引擎
	probe     service.ProbeService         // 4.1 TLS 探测（台账全部 sans）
	publisher service.CertAlertPublisher   // 异常/tls_diff 事件发布（4.3 通道）
	store     InspectionRunStore           // 运行记录（lastInspectionAt+指标）
	now       func() time.Time             // 时钟注入缝（测试固定 run.At）
	mu        sync.Mutex
	running   bool // 单飞幂等守卫
}

// NewInspectionJob 创建巡检 Job。deps 说明：
//   - certs：台账（完整性复检复跑 2.1 解析校验）
//   - exempts：豁免清单（豁免过滤：豁免域名探测记 exempt 不告警）
//   - expiry/probe：4.2/4.1 已就绪服务，本 Job 仅编排
//   - publisher：完整性异常（ops 类）与 tls_diff 事件发布；nil 回退日志实现
//   - store：运行记录；nil 回退内存实现（进程内 lastInspectionAt）
func NewInspectionJob(
	certs domain.CertificateRepository,
	exempts domain.ExemptionRepository,
	expiry service.InspectionService,
	probe service.ProbeService,
	publisher service.CertAlertPublisher,
	store InspectionRunStore,
) *InspectionJob {
	if publisher == nil {
		publisher = service.NewLoggingAlertPublisher()
	}
	if store == nil {
		store = NewMemoryInspectionRunStore()
	}
	return &InspectionJob{
		certs:     certs,
		exempts:   exempts,
		expiry:    expiry,
		probe:     probe,
		publisher: publisher,
		store:     store,
		now:       time.Now,
	}
}

// RunInspection 执行一轮巡检（AC1 四步顺序）。返回运行记录（含各子步指标）与
// 聚合错误（步骤级失败不中断后续步骤；单证/单域失败在步骤内部吸收为指标计数）。
//
// 幂等：同进程并发触发被单飞守卫拒绝（ErrInspectionInProgress）；同日重复执行
// 的到期告警去重由 4.2 状态机保证；tls_diff 按探测事件逐轮触发（设计口径：四类
// 中仅到期分级需要跨巡检去重）。运行记录无条件落（部分失败也记录，监控可见），
// 记录调用脱离原 ctx 取消（WithoutCancel）保证收尾不丢。
func (j *InspectionJob) RunInspection(ctx context.Context) (InspectionRun, error) {
	if !j.acquire() {
		return InspectionRun{}, ErrInspectionInProgress
	}
	defer j.release()

	run := InspectionRun{At: j.now(), Steps: make([]StepMetrics, 0, 4)}

	// 步骤一：完整性复检（异常项 ops 事件，不阻塞后续）
	integrity, errIntegrity := j.stepIntegrityRecheck(ctx)
	run.Steps = append(run.Steps, integrity)

	// 步骤二：到期分级计算与去重告警（4.2）
	expiry, errExpiry := j.stepExpiryTiering(ctx)
	run.Steps = append(run.Steps, expiry)

	// 步骤三：TLS 探测调度（4.1，台账全部 sans）
	probe, results, errProbe := j.stepProbe(ctx)
	run.Steps = append(run.Steps, probe)

	// 步骤四：豁免过滤（exempt 不告警；常规 diff 发 tls_diff）
	exemption, errExemption := j.stepExemptionFilter(ctx, results)
	run.Steps = append(run.Steps, exemption)

	err := errors.Join(errIntegrity, errExpiry, errProbe, errExemption)

	// 运行记录：lastInspectionAt（dashboard）+ 各子步指标（平台自身监控）
	if recErr := j.store.RecordRun(context.WithoutCancel(ctx), run); recErr != nil {
		err = errors.Join(err, fmt.Errorf("inspection: record run: %w", recErr))
	}
	j.logRound(run, err)
	return run, err
}

// acquire 单飞守卫：已在执行返回 false。
func (j *InspectionJob) acquire() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.running {
		return false
	}
	j.running = true
	return true
}

// release 释放单飞守卫。
func (j *InspectionJob) release() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.running = false
}

// logRound 输出各子步成功率指标（结构化日志，供平台自身监控采集）。
func (j *InspectionJob) logRound(run InspectionRun, err error) {
	steps := make([]map[string]any, 0, len(run.Steps))
	for _, s := range run.Steps {
		steps = append(steps, map[string]any{
			"name": s.Name, "ok": s.Ok,
			"total": s.Total, "failed": s.Failed,
			"successRate": s.SuccessRate(), "extra": s.Extra,
		})
	}
	slog.Info("cert inspection round finished",
		slog.Time("at", run.At),
		slog.Bool("ok", err == nil && run.Ok()),
		slog.Any("steps", steps),
	)
	if err != nil {
		slog.Error("cert inspection round has failures", slog.Time("at", run.At), slog.Any("err", err.Error()))
	}
}

// ---------------------------------------------------------------------
// 步骤一：完整性复检（AC2）
// ---------------------------------------------------------------------

// stepIntegrityRecheck 复跑 2.1 校验存储证书：可解析/未过期/链完整（keyPEM 传
// nil——完整性复检不解密私钥，仅校验证书束；复用 domain.ParseCertAndKey 纯函数）
// 并核对解析指纹与台账一致。异常项发布 ops 类处置通知（经 4.3 通道、category
// 区分，不计入业务四类告警），单证失败不中断整轮。
func (j *InspectionJob) stepIntegrityRecheck(ctx context.Context) (StepMetrics, error) {
	m := StepMetrics{Name: StepIntegrity, Ok: true, Extra: map[string]int{}}
	certs, err := j.certs.List(ctx)
	if err != nil {
		m.Ok = false
		return m, fmt.Errorf("inspection: integrity list ledger: %w", err)
	}
	m.Total = len(certs)

	var errs []error
	anomalies := 0
	now := j.now()
	for _, cert := range certs {
		if err := ctx.Err(); err != nil {
			m.Ok = false
			errs = append(errs, fmt.Errorf("inspection: integrity round cancelled: %w", err))
			break
		}
		reason := integrityAnomaly(cert)
		if reason == "" {
			continue
		}
		anomalies++
		if pubErr := j.publisher.PublishAlert(ctx, service.CertAlertEvent{
			Category:    service.AlertCategoryOps,
			Title:       integrityTitle,
			Fingerprint: cert.Fingerprint,
			SANs:        cert.Sans,
			Detail:      reason, // 静态文案/时间等安全参数，不含私钥材料
			At:          now,
		}); pubErr != nil {
			errs = append(errs, fmt.Errorf("inspection: publish integrity anomaly for %s: %w", cert.Fingerprint, pubErr))
		}
	}
	m.Failed = anomalies
	m.Extra["anomalies"] = anomalies
	if len(errs) > 0 {
		m.Ok = false // 步骤级错误（取消/发布失败）；单证异常本身不算步骤失败
	}
	return m, errors.Join(errs...)
}

// integrityAnomaly 单证完整性复检：返回异常原因（空=通过）。
// 错误文案来自 domain.ParseCertAndKey（静态文案与时间/算法名，无敏感材料）。
func integrityAnomaly(cert domain.Certificate) string {
	if cert.CertPEM == "" {
		return "stored certificate bundle missing (certPem empty)"
	}
	parsed, err := domain.ParseCertAndKey([]byte(cert.CertPEM), nil)
	if err != nil {
		return fmt.Sprintf("stored certificate failed integrity recheck: %v", err)
	}
	if parsed.Fingerprint != cert.Fingerprint {
		return "stored certificate fingerprint differs from ledger entry"
	}
	return ""
}

// ---------------------------------------------------------------------
// 步骤二：到期分级计算与去重告警（4.2 编排）
// ---------------------------------------------------------------------

// stepExpiryTiering 调 4.2 InspectLedger：逐证计级 → 升级去重判定 → 到期分级
// 事件发布 → 去重状态持久化。单证失败在引擎内部聚合（不中断其他证）。
func (j *InspectionJob) stepExpiryTiering(ctx context.Context) (StepMetrics, error) {
	m := StepMetrics{Name: StepExpiry, Ok: true, Extra: map[string]int{}}
	summary, err := j.expiry.InspectLedger(ctx)
	if err != nil {
		m.Ok = false
	}
	m.Total = summary.Evaluated
	m.Extra["triggered"] = summary.Triggered
	m.Extra["reset"] = summary.Reset
	if err != nil {
		return m, fmt.Errorf("inspection: expiry tiering: %w", err)
	}
	return m, nil
}

// ---------------------------------------------------------------------
// 步骤三：TLS 探测调度（4.1 编排，台账全部 sans）
// ---------------------------------------------------------------------

// stepProbe 调 4.1 探测：DNS 源可用时优先 ProbeAllTenantDNS（按租户轮，覆盖通配符证书
// 实际子域名部署）；未装配回退 ProbeLedgerDomains（台账全部 sans 展开去重）。
// unreachable 计入 Failed（拨测失败域，平台监控关注），其余状态为业务语义不计失败。
// 单域失败在探测服务内部吸收。
func (j *InspectionJob) stepProbe(ctx context.Context) (StepMetrics, []domain.ProbeResult, error) {
	m := StepMetrics{Name: StepProbe, Ok: true, Extra: map[string]int{}}
	results, err := j.probe.ProbeAllTenantDNS(ctx)
	if errors.Is(err, service.ErrNoDNSSource) {
		// DNS 源未装配：回退台账 SAN 路径
		results, err = j.probe.ProbeLedgerDomains(ctx)
	}
	if err != nil {
		m.Ok = false
	}
	m.Total = len(results)
	for _, r := range results {
		m.Extra[string(r.Status)]++
		if r.Status == domain.ProbeStatusUnreachable {
			m.Failed++
		}
	}
	if err != nil {
		return m, results, fmt.Errorf("inspection: tls probe: %w", err)
	}
	return m, results, nil
}

// ---------------------------------------------------------------------
// 步骤四：豁免过滤（exempt 不告警；常规 diff 发 tls_diff）
// ---------------------------------------------------------------------

// stepExemptionFilter 对探测结果做告警过滤：豁免域名探测记 exempt、不告警
// （豁免标记由 4.1 探测时落定，本步核对计数）；仅常规 diff 发布 tls_diff 事件
// （按探测事件触发，无跨巡检去重——四类中仅到期分级跨巡检去重）。
// unreachable（不参与差异告警）、wildcard_skipped（不拨测）、change_linked_diff
// （验证窗口预期切换，由 5.10 变更关联通道触达）均不发布。
func (j *InspectionJob) stepExemptionFilter(ctx context.Context, results []domain.ProbeResult) (StepMetrics, error) {
	m := StepMetrics{Name: StepExemption, Ok: true, Total: len(results), Extra: map[string]int{}}
	exemptions, err := j.exempts.List(ctx)
	if err != nil {
		m.Ok = false
		return m, fmt.Errorf("inspection: list exemptions: %w", err)
	}
	exemptSet := make(map[string]bool, len(exemptions))
	for _, e := range exemptions {
		exemptSet[e.Domain] = true
	}

	var errs []error
	now := j.now()
	for _, r := range results {
		switch r.Status {
		case domain.ProbeStatusDiff:
			// 常规差异 → tls_diff 事件（域粒度；Fingerprint 留空——事件指纹字段
			// 为台账归属证口径，探测结果不携带归属映射，线上指纹入 Detail）
			if pubErr := j.publisher.PublishAlert(ctx, service.CertAlertEvent{
				Category: service.AlertCategoryTLSDiff,
				Title:    fmt.Sprintf(diffTitleFmt, r.Domain),
				Domain:   r.Domain,
				Detail:   fmt.Sprintf("online fingerprint %s differs from ledger", r.OnlineFingerprint),
				At:       now,
			}); pubErr != nil {
				errs = append(errs, fmt.Errorf("inspection: publish tls diff for %s: %w", r.Domain, pubErr))
				continue
			}
			m.Extra["diffAlerted"]++
		case domain.ProbeStatusExempt:
			// 豁免域名仍探测、记 exempt、不告警（AC：豁免过滤）
			m.Extra["exemptProbed"]++
		case domain.ProbeStatusUnreachable:
			// 豁免域拨测失败保留 unreachable（真可达性，不参与差异告警）
			if exemptSet[r.Domain] {
				m.Extra["exemptUnreachable"]++
			}
		default:
			// consistent / wildcard_skipped / change_linked_diff：不发布（见函数注释）
		}
	}
	if len(errs) > 0 {
		m.Ok = false // 步骤级错误（清单读取失败已在上方早退/事件发布失败）
	}
	return m, errors.Join(errs...)
}

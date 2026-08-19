package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// ---------------------------------------------------------------------
// 验证窗口服务（任务 5.10，tech-design"验证窗口告警路由"+"分批执行门控"+
// Scheduler Tasks window-expiry 行）：
//
//	批执行完成时固化 verifyExpected{newCertFingerprint, domains[], excludedDomains[],
//	windowUntil}（豁免域名剔除入 excludedDomains，验证项计 skipped 不阻塞达标——
//	防"含豁免域名的窗口永不达标"死锁）；窗口内对 verifyExpected.domains 提频探测
//	（7.1 以 verifyProbeIntervalMinutes 周期调度 ProbeVerifyingWindows），连续
//	verifyConfirmProbes 次线上指纹=预期指纹 = 达标；window-expiry 终局判定
//	（FinalizeExpiredWindows，Hard Rule：不依赖被动探测触发——scheduler 主动扫描
//	verifyWindowUntil <= now 且 status=verifying 单据收敛终态）；窗口内
//	change_linked_diff 差异走变更关联通道（VerifyWindowContext.Active=true），
//	窗口关闭后恢复常规 diff 告警（Active=false，4.3 路由）。
//
// 消费 5.7 交付缝：SealVerifyExpected 经 VerifyWindowSealer 注入
// changeExecuteService.enterVerifyWindow（EnterVerify 原子迁移后立即固化）；
// BatchVerified 实现 BatchVerifyChecker（ConfirmBatch 门控 2）。
// ---------------------------------------------------------------------

// VerifyWindowRecorder 验证窗口结果写入端口（任务 5.10）。
//
// tech-design 定义 ChangeReport 为非持久化载荷（GetReport 查询时聚合），故
// UnmetDomains 落库经本端口承载（同 5.9 OrphanCleanupRecorder 口径）：生产实现
// 由 7.x 接线（5.11 报告聚合消费）；nil=no-op（终局判定仍执行，结果仅余订单
// 状态与告警）。幂等契约：实现方以 (orderID, at) 为去重键，重复记录返回
// recorded=false。
type VerifyWindowRecorder interface {
	// RecordUnmetDomains 记录窗口关闭未达标域名清单（partial_completed 时非空）；
	// 返回是否新写入（false=同键已存在）。
	RecordUnmetDomains(ctx context.Context, orderID string, unmetDomains []string, at time.Time) (bool, error)
}

// VerifyWindowTransitions 终态迁移端口：复用 ChangeService.Transition（白名单
// 校验 + completed/partial_completed 保护期固化 + activeMutex 原子清除）。
// 窄接口避免验证窗口服务耦合完整生命周期服务（依赖倒置，7.x 装配注入）。
type VerifyWindowTransitions interface {
	Transition(ctx context.Context, orderID string, target domain.ChangeStatus) error
}

// VerifyWindowService 验证窗口服务（AC-1~AC-5）。
type VerifyWindowService interface {
	// SealVerifyExpected 固化验证窗口预期终态快照（AC-1）：批执行完成进入
	// verifying 后调用——domains = 订单目标域名（旧证书 SAN 集合，5.2 SAN 预检
	// 同基准）剔除当前豁免清单命中域名（记入 excludedDomains，计 skipped 不参与
	// 达标判定）；newCertFingerprint = 新证书指纹；windowUntil = now +
	// thresholds.verifyWindowHours。分批单每批进入 verifying 时覆盖刷新（固化后
	// 不随台账变化）。CAS status=verifying，未命中（状态已并发迁移）返回 false。
	SealVerifyExpected(ctx context.Context, orderID string) (bool, error)
	// ProbeVerifyingWindows 窗口内提频探测（AC-2，7.1 周期=verifyProbeIntervalMinutes
	// 调度）：对全部验证中单（status=verifying 且窗口未过期）的
	// verifyExpected.domains 执行一轮探测（复用 4.1 ProbeDomains），并做达标
	// 判定——达标即窗口提前收敛（终批/未分批→completed；非终批→批间暂停
	// paused=true/pausedAt 等 ConfirmBatch 续批）；未达标时对新转入
	// change_linked_diff 的域名经变更关联通道发布告警（附 orderId/预期指纹/
	// 达标计数）。verifyExpected 缺失时惰性补固化（固化缝未接线的自愈路径）。
	// 返回本轮探测订单数；单笔失败不中断扫描，首批错误随计数返回。
	ProbeVerifyingWindows(ctx context.Context) (int, error)
	// BatchVerified 批级验证达标判定（5.7 ConfirmBatch 门控 2 消费）：
	// verifyExpected.domains 全部域名最近连续 verifyConfirmProbes 次线上指纹 =
	// 预期指纹 = 达标（豁免域名构建期已剔除；无 override 通配符计 skipped 不
	// 阻塞——Hard Rule）。reason 为安全文案（409 上下文，仅域名与计数）；
	// err 非 nil 时调用方按门控未满足处理（安全侧不放行）。
	BatchVerified(ctx context.Context, order domain.ChangeOrder) (bool, string, error)
	// FinalizeExpiredWindows window-expiry 终局判定（AC-3，Hard Rule：不依赖
	// 被动探测触发）：扫描 verifyWindowUntil <= now 且 status=verifying 单据——
	// 全部达标→completed；未达标：终批/未分批→partial_completed + 未达标域名
	// 写入 ChangeReport.UnmetDomains（Verify.Unmet 计数）+ 恢复常规 diff 告警
	//（change_linked 事件 Active=false，4.3 路由恢复常规通道）；非终批→批间
	// 暂停（verifying→executing+paused，转人工决策：回滚/Cancel，不自动续批）。
	// 返回收敛订单 ID 清单；单笔失败不中断扫描，首批错误随清单一并返回。
	FinalizeExpiredWindows(ctx context.Context) ([]string, error)
}

type verifyWindowService struct {
	orders    domain.ChangeOrderRepository
	certs     domain.CertificateRepository
	exempts   domain.ExemptionRepository
	alertCfg  domain.AlertConfigRepository
	probes    domain.ProbeResultRepository
	prober    ProbeService            // 提频探测复用 4.1 ProbeDomains
	changes   VerifyWindowTransitions // 终态迁移（completed/partial_completed）
	recorder  VerifyWindowRecorder    // UnmetDomains 报告写入；nil=no-op
	publisher CertAlertPublisher      // 变更关联/恢复常规告警；nil=日志发布
	now       func() time.Time        // 测试可注入时间源
}

// NewVerifyWindowService 创建验证窗口服务。prober 为 4.1 探测服务实例；
// changes 为 ChangeService（或等价 Transition 实现）；recorder/publisher 为
// nil 时分别回退 no-op / 日志发布（终局判定不依赖报告与告警成功）。
func NewVerifyWindowService(
	orders domain.ChangeOrderRepository,
	certs domain.CertificateRepository,
	exempts domain.ExemptionRepository,
	alertCfg domain.AlertConfigRepository,
	probes domain.ProbeResultRepository,
	prober ProbeService,
	changes VerifyWindowTransitions,
	recorder VerifyWindowRecorder,
	publisher CertAlertPublisher,
) VerifyWindowService {
	if publisher == nil {
		publisher = NewLoggingAlertPublisher()
	}
	return &verifyWindowService{
		orders:    orders,
		certs:     certs,
		exempts:   exempts,
		alertCfg:  alertCfg,
		probes:    probes,
		prober:    prober,
		changes:   changes,
		recorder:  recorder,
		publisher: publisher,
		now:       time.Now,
	}
}

// 编译期断言：满足 5.7 交付的两个消费缝。
var (
	_ BatchVerifyChecker = (*verifyWindowService)(nil)
	_ VerifyWindowSealer = (*verifyWindowService)(nil)
)

// ---------------------------------------------------------------------
// AC-1：verifyExpected 固化
// ---------------------------------------------------------------------

// SealVerifyExpected 固化验证窗口预期终态快照。
func (s *verifyWindowService) SealVerifyExpected(ctx context.Context, orderID string) (bool, error) {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return false, fmt.Errorf("verify window: get order: %w", err)
	}
	if order.Status != domain.ChangeStatusVerifying {
		return false, nil // 非验证中（并发迁移/误用）：CAS 语义下的幂等未命中
	}
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return false, fmt.Errorf("verify window: get alert config: %w", err)
	}
	newCert, err := s.certs.GetByID(ctx, order.NewCertID)
	if err != nil {
		return false, fmt.Errorf("verify window: load new certificate %s: %w", order.NewCertID, err)
	}
	// 目标域名基准 = 旧证书 SAN（5.2 SAN 预检同口径：清单项所服务域名并集）
	oldCert, err := s.certs.GetByFingerprint(ctx, order.OldCertFingerprint)
	if err != nil {
		return false, fmt.Errorf("verify window: load old certificate for target domains: %w", err)
	}
	exemptions, err := s.exempts.List(ctx)
	if err != nil {
		return false, fmt.Errorf("verify window: list exemptions: %w", err)
	}
	windowUntil := s.now().Add(time.Duration(cfg.Thresholds.VerifyWindowHours) * time.Hour)
	expected := buildVerifyExpected(oldCert.Sans, exemptions, newCert.Fingerprint, windowUntil)
	ok, err := s.orders.SetVerifyExpected(ctx, orderID, expected)
	if err != nil {
		return false, fmt.Errorf("verify window: solidify verifyExpected for order %s: %w", orderID, err)
	}
	return ok, nil
}

// buildVerifyExpected 构建验证窗口预期终态快照（纯函数，AC-1）：
// domains = 目标域名保序去重后剔除豁免清单命中项（剔除项记入 excludedDomains，
// 验证计 skipped 不参与达标判定——Hard Rule 防"含豁免域名的窗口永不达标"死锁）。
func buildVerifyExpected(
	targetDomains []string,
	exemptions []domain.Exemption,
	newCertFingerprint string,
	windowUntil time.Time,
) *domain.VerifyExpected {
	exemptSet := make(map[string]bool, len(exemptions))
	for _, e := range exemptions {
		exemptSet[e.Domain] = true
	}
	seen := make(map[string]bool, len(targetDomains))
	domains := make([]string, 0, len(targetDomains))
	excluded := make([]string, 0)
	for _, raw := range targetDomains {
		name := strings.TrimSpace(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if exemptSet[name] {
			excluded = append(excluded, name)
			continue
		}
		domains = append(domains, name)
	}
	return &domain.VerifyExpected{
		NewCertFingerprint: newCertFingerprint,
		Domains:            domains,
		ExcludedDomains:    excluded,
		WindowUntil:        windowUntil,
	}
}

// ---------------------------------------------------------------------
// 达标判定（AC-2/AC-3/AC-5 共用）
// ---------------------------------------------------------------------

// verifyTarget 单个验证项的判定结果。
type verifyTarget struct {
	Domain string // verifyExpected.domains 原条目（报告/告警口径）
	Judged string // 实际判定域名（通配符 override 子域名或原条目）
	Met    bool   // 最近连续 verifyConfirmProbes 次线上指纹 = 预期指纹
	Streak int    // 当前连续一致次数（change_linked 告警 PassCount 上下文）
	// Latest/Prev 最近两次探测状态（nil=无记录）；窗口内 change_linked 告警的
	// 去重依据——仅新转入 change_linked_diff 的域名告警（状态迁移事件语义）。
	Latest *domain.ProbeStatus
	Prev   *domain.ProbeStatus
}

// verifyJudge 窗口达标判定结果：Met=全部验证项达标（skipped 项不阻塞）。
type verifyJudge struct {
	Met            bool
	UnmetDomains   []string // 未达标域名（保持 domains 次序；豁免/通配符 skipped 项不在内）
	SkippedDomains []string // 计 skipped 验证项（无 override 通配符；豁免域名构建期已剔除）
	Targets        []verifyTarget
}

// probeTarget 变更验证拨测判定目标（judgeWindow / summarizeVerify 共用解析）。
type probeTarget struct {
	domain  string               // verifyExpected.domains 原条目（报告/告警口径）
	target  string               // 实际判定域名（通配符 override 子域名或原条目）
	results []domain.ProbeResult // 最近 confirmProbes+1 条记录（probeAt 降序）
	streak  int                  // 头部连续等于预期指纹的记录数
}

// resolveVerifyTargets 解析验证判定目标并取探测记录（verify window 与
// change query 同口径）：通配符按 wildcardProbeOverrides 替换（无 override
// 计入 skipped、不阻塞达标），多通配符 override 到同一子域名去重；每目标取
// 最近 confirmProbes+1 条记录（+1 条供状态迁移判定）并计算与预期指纹的
// 连续一致条数。confirmProbes<=0 回退 DefaultThresholds。
func resolveVerifyTargets(
	ctx context.Context,
	probes domain.ProbeResultRepository,
	expected *domain.VerifyExpected,
	cfg domain.AlertConfig,
) (targets []probeTarget, skipped []string, confirmProbes int, err error) {
	confirmProbes = cfg.Thresholds.VerifyConfirmProbes
	if confirmProbes <= 0 {
		confirmProbes = domain.DefaultThresholds().VerifyConfirmProbes
	}
	targets = make([]probeTarget, 0, len(expected.Domains))
	skipped = make([]string, 0)
	seen := make(map[string]bool, len(expected.Domains))
	for _, d := range expected.Domains {
		name := strings.TrimSpace(d)
		if name == "" {
			continue
		}
		if isWildcardSAN(name) {
			if sub, ok := cfg.WildcardProbeOverrides[name]; ok && strings.TrimSpace(sub) != "" {
				name = strings.TrimSpace(sub) // 结果记于子域名（4.1 同口径）
			} else {
				skipped = append(skipped, name)
				continue // 无 override 通配符无法拨测：计 skipped，不阻塞达标
			}
		}
		if seen[name] {
			continue // 多通配符 override 到同一子域名等去重
		}
		seen[name] = true
		targets = append(targets, probeTarget{domain: d, target: name})
	}
	if len(targets) == 0 {
		return targets, skipped, confirmProbes, nil
	}
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		names = append(names, t.target)
	}
	recent, err := probes.ListRecentByDomains(ctx, names, confirmProbes+1)
	if err != nil {
		return nil, skipped, confirmProbes, fmt.Errorf("list recent probes: %w", err)
	}
	byTarget := make(map[string][]domain.ProbeResult, len(targets))
	for _, r := range recent {
		byTarget[r.Domain] = append(byTarget[r.Domain], r)
	}
	for i := range targets {
		results := byTarget[targets[i].target]
		streak := 0
		for _, r := range results { // probeAt 降序：遇首条不一致即断
			if r.OnlineFingerprint == expected.NewCertFingerprint {
				streak++
			} else {
				break
			}
		}
		targets[i].results = results
		targets[i].streak = streak
	}
	return targets, skipped, confirmProbes, nil
}

// judgeWindow 判定 verifyExpected.domains 达标情况（AC-2/AC-5）：
//   - 通配符 SAN 无 concreteSubdomainOverride → 计 skipped 不阻塞（Hard Rule，
//     同豁免语义）；有 override → 以子域名探测结果判定（4.1 目标解析同口径）；
//   - 达标 = 该域名最近连续 verifyConfirmProbes 条探测记录 onlineFingerprint
//     均等于 newCertFingerprint（记录不足阈值次数视为未达标——"连续 N 次一致"
//     不可判定）；探测记录状态覆盖 change_linked_diff（新证未入台账）与
//     consistent（新证已入台账）两种一致形态（按指纹判定，不依赖状态枚举）。
func (s *verifyWindowService) judgeWindow(ctx context.Context, expected *domain.VerifyExpected, cfg domain.AlertConfig) (verifyJudge, error) {
	judge := verifyJudge{}
	if expected == nil {
		return judge, fmt.Errorf("verify window: verifyExpected snapshot is nil")
	}
	targets, skipped, confirmProbes, err := resolveVerifyTargets(ctx, s.probes, expected, cfg)
	if err != nil {
		return judge, fmt.Errorf("verify window: %w", err)
	}
	judge.SkippedDomains = skipped
	if len(targets) == 0 {
		judge.Met = true // 全部 skipped：窗口仍可正常达标（Hard Rule）
		return judge, nil
	}

	unmet := make([]string, 0)
	for _, t := range targets {
		vt := verifyTarget{Domain: t.domain, Judged: t.target, Streak: t.streak, Met: t.streak >= confirmProbes}
		if len(t.results) > 0 {
			latest := t.results[0].Status
			vt.Latest = &latest
		}
		if len(t.results) > 1 {
			prev := t.results[1].Status
			vt.Prev = &prev
		}
		judge.Targets = append(judge.Targets, vt)
		if !vt.Met {
			unmet = append(unmet, t.domain)
		}
	}
	judge.UnmetDomains = unmet
	judge.Met = len(unmet) == 0
	return judge, nil
}

// isFinalBatch 终批判定：未分批（单批全量）或当前批即末批。
func isFinalBatch(order domain.ChangeOrder) bool {
	return order.BatchInfo == nil || order.BatchInfo.CurrentBatch >= order.BatchInfo.TotalBatches
}

// ---------------------------------------------------------------------
// AC-2：窗口内提频探测 + 提前达标关闭
// ---------------------------------------------------------------------

// ProbeVerifyingWindows 提频探测入口（7.1 周期=verifyProbeIntervalMinutes 调度）。
func (s *verifyWindowService) ProbeVerifyingWindows(ctx context.Context) (int, error) {
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("verify window: get alert config: %w", err)
	}
	verifying, err := s.orders.ListVerifyingActive(ctx, s.now())
	if err != nil {
		return 0, fmt.Errorf("verify window: list verifying orders: %w", err)
	}
	probed := 0
	var firstErr error
	for _, order := range verifying {
		expected := order.VerifyExpected
		if expected == nil {
			// 惰性补固化（固化缝未接线/固化写失败的自愈；仅缺失时补，不覆盖既有快照）
			var serr error
			expected, serr = s.lazySeal(ctx, &order)
			if serr != nil {
				if firstErr == nil {
					firstErr = serr
				}
				continue
			}
			if expected == nil {
				continue // 并发迁移（续批/终局收敛），幂等跳过
			}
		}
		if len(expected.Domains) > 0 {
			if _, perr := s.prober.ProbeDomains(ctx, expected.Domains); perr != nil {
				// 单域名拨测/写库失败不中断整轮（4.1 逐项隔离）；部分结果已落库，
				// 继续按既有数据判定
				if firstErr == nil {
					firstErr = fmt.Errorf("verify window: probe order %s: %w", order.ID.Hex(), perr)
				}
			}
		}
		probed++
		judge, jerr := s.judgeWindow(ctx, expected, cfg)
		if jerr != nil {
			if firstErr == nil {
				firstErr = jerr
			}
			continue
		}
		if judge.Met {
			if cerr := s.closeMetWindow(ctx, order); cerr != nil && firstErr == nil {
				firstErr = cerr
			}
			continue
		}
		// 未达标：窗口内 change_linked 差异走变更关联通道（新转入域名去重告警）
		if aerr := s.publishChangeLinkedAlerts(ctx, order, expected, judge); aerr != nil && firstErr == nil {
			firstErr = aerr
		}
	}
	return probed, firstErr
}

// lazySeal verifyExpected 缺失时补固化（返回补固化后的快照；不覆盖既有快照）。
func (s *verifyWindowService) lazySeal(ctx context.Context, order *domain.ChangeOrder) (*domain.VerifyExpected, error) {
	sealed, err := s.SealVerifyExpected(ctx, order.ID.Hex())
	if err != nil {
		return nil, err
	}
	if !sealed {
		return nil, nil // 状态已并发迁移：无窗口可判
	}
	refreshed, err := s.orders.GetByID(ctx, order.ID.Hex())
	if err != nil {
		return nil, fmt.Errorf("verify window: reload order %s after seal: %w", order.ID.Hex(), err)
	}
	*order = refreshed
	return refreshed.VerifyExpected, nil
}

// closeMetWindow 窗口提前达标关闭（AC-2）：
//   - 终批/未分批：completed（保护期固化 + activeMutex 原子清除，经
//     VerifyWindowTransitions 复用 ChangeService 语义）；
//   - 非终批：批级达标 → 批间暂停（verifying→executing + paused=true/pausedAt，
//     等 ConfirmBatch 人工续批；activeMutex 全程持有）。
func (s *verifyWindowService) closeMetWindow(ctx context.Context, order domain.ChangeOrder) error {
	id := order.ID.Hex()
	if isFinalBatch(order) {
		if err := s.changes.Transition(ctx, id, domain.ChangeStatusCompleted); err != nil {
			return fmt.Errorf("verify window: complete order %s on met window: %w", id, err)
		}
		return nil
	}
	ok, err := s.orders.PauseAfterVerify(ctx, id, s.now())
	if err != nil {
		return fmt.Errorf("verify window: pause order %s after met batch verify: %w", id, err)
	}
	if !ok {
		return nil // 并发迁移（ConfirmBatch/终局收敛），幂等
	}
	return nil
}

// publishChangeLinkedAlerts 窗口内变更关联告警（AC-4）：本轮探测新转入
// change_linked_diff 的域名（最近一次为 change_linked_diff 且前一次非
// change_linked_diff 或无历史——状态迁移事件语义，持续差异不重复告警）经
// 变更关联通道发布（VerifyWindow.Active=true，附 orderId/预期指纹/达标计数）。
func (s *verifyWindowService) publishChangeLinkedAlerts(
	ctx context.Context,
	order domain.ChangeOrder,
	expected *domain.VerifyExpected,
	judge verifyJudge,
) error {
	for _, tgt := range judge.Targets {
		if tgt.Latest == nil || *tgt.Latest != domain.ProbeStatusChangeLinkedDiff {
			continue
		}
		if tgt.Prev != nil && *tgt.Prev == domain.ProbeStatusChangeLinkedDiff {
			continue // 持续 change_linked_diff：已告警过，不重复
		}
		at := s.now()
		if err := s.publisher.PublishAlert(ctx, CertAlertEvent{
			Category:    AlertCategoryChangeLinked,
			Title:       "验证窗口：域名已切换至新证书（变更关联差异）",
			Fingerprint: order.OldCertFingerprint,
			Domain:      tgt.Judged,
			OrderID:     order.ID.Hex(),
			Detail: fmt.Sprintf("域名 %s 线上指纹已切换为变更预期指纹（连续一致 %d 次）",
				tgt.Judged, tgt.Streak),
			At: at,
			VerifyWindow: &VerifyWindowContext{
				Active:              true,
				OrderID:             order.ID.Hex(),
				ExpectedFingerprint: expected.NewCertFingerprint,
				PassCount:           tgt.Streak,
			},
		}); err != nil {
			return fmt.Errorf("verify window: publish change_linked alert for domain %s: %w", tgt.Judged, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------
// 5.7 ConfirmBatch 门控 2：批级验证达标判定
// ---------------------------------------------------------------------

// BatchVerified 批级验证达标判定（BatchVerifyChecker 实现）。
func (s *verifyWindowService) BatchVerified(ctx context.Context, order domain.ChangeOrder) (bool, string, error) {
	if order.VerifyExpected == nil {
		return false, "批级验证未达标：验证窗口预期终态未固化（verifyExpected 缺失）", nil
	}
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return false, "", fmt.Errorf("verify window: get alert config: %w", err)
	}
	judge, err := s.judgeWindow(ctx, order.VerifyExpected, cfg)
	if err != nil {
		return false, "", err
	}
	if judge.Met {
		return true, "", nil
	}
	return false, verifyUnmetReason(judge, cfg.Thresholds.VerifyConfirmProbes), nil
}

// verifyUnmetReason 未达标安全文案（409 上下文：仅域名与计数，至多列 5 个域名）。
func verifyUnmetReason(judge verifyJudge, confirmProbes int) string {
	shown := judge.UnmetDomains
	if len(shown) > 5 {
		shown = append([]string(nil), shown[:5]...)
		shown = append(shown, "等")
	}
	return fmt.Sprintf("批级验证未达标（连续一致阈值 %d 次）：%s", confirmProbes, strings.Join(shown, ", "))
}

// ---------------------------------------------------------------------
// AC-3：window-expiry 终局判定
// ---------------------------------------------------------------------

// FinalizeExpiredWindows 窗口到期终局判定（Hard Rule：scheduler 主动扫描，
// 不依赖被动探测触发）。
func (s *verifyWindowService) FinalizeExpiredWindows(ctx context.Context) ([]string, error) {
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("verify window: get alert config: %w", err)
	}
	expired, err := s.orders.ListVerifyingExpired(ctx, s.now())
	if err != nil {
		return nil, fmt.Errorf("verify window: list expired verifying orders: %w", err)
	}
	finalized := make([]string, 0, len(expired))
	var firstErr error
	for _, order := range expired {
		expected := order.VerifyExpected
		if expected == nil {
			// 终局判定前提：快照缺失时补固化（按当前台账/豁免构建——退化路径，
			// 固化缝正常接线时不可达）
			var serr error
			expected, serr = s.lazySeal(ctx, &order)
			if serr != nil {
				if firstErr == nil {
					firstErr = serr
				}
				continue
			}
			if expected == nil {
				continue
			}
		}
		judge, jerr := s.judgeWindow(ctx, expected, cfg)
		if jerr != nil {
			if firstErr == nil {
				firstErr = jerr
			}
			continue
		}
		if ferr := s.finalizeWindow(ctx, order, expected, judge, cfg.Thresholds.VerifyConfirmProbes); ferr != nil {
			if firstErr == nil {
				firstErr = ferr
			}
			continue
		}
		finalized = append(finalized, order.ID.Hex())
	}
	return finalized, firstErr
}

// finalizeWindow 单笔窗口收敛。
func (s *verifyWindowService) finalizeWindow(
	ctx context.Context,
	order domain.ChangeOrder,
	expected *domain.VerifyExpected,
	judge verifyJudge,
	confirmProbes int,
) error {
	id := order.ID.Hex()
	if !isFinalBatch(order) {
		// 批级窗口到期：回批间暂停转人工决策（回滚成功项 / Cancel 取消，不自动
		// 续批——ConfirmBatch 门控 2 仍校验达标）；未达标域名恢复常规跟踪。
		ok, err := s.orders.PauseAfterVerify(ctx, id, s.now())
		if err != nil {
			return fmt.Errorf("verify window: pause order %s after batch window expiry: %w", id, err)
		}
		if ok {
			if aerr := s.publishRestoreAlerts(ctx, order, expected, judge, confirmProbes); aerr != nil {
				return aerr
			}
		}
		return nil // CAS 未命中（并发续批/取消）：幂等
	}
	if judge.Met {
		if err := s.changes.Transition(ctx, id, domain.ChangeStatusCompleted); err != nil {
			return fmt.Errorf("verify window: complete order %s: %w", id, err)
		}
		return nil
	}
	// 终批未达标：partial_completed + UnmetDomains + 恢复常规 diff 告警。
	// 次序：终态先落地（token 清除，防终局判定重扫），报告与告警随后——
	// 失败上抛供调度方感知，不回滚终态（下轮扫描不再命中该单）。
	if err := s.changes.Transition(ctx, id, domain.ChangeStatusPartialCompleted); err != nil {
		return fmt.Errorf("verify window: partial-complete order %s: %w", id, err)
	}
	if rerr := s.recordUnmet(ctx, id, judge.UnmetDomains); rerr != nil {
		return rerr
	}
	return s.publishRestoreAlerts(ctx, order, expected, judge, confirmProbes)
}

// recordUnmet 未达标清单写入（nil recorder 或空清单=no-op）。
func (s *verifyWindowService) recordUnmet(ctx context.Context, orderID string, unmet []string) error {
	if s.recorder == nil || len(unmet) == 0 {
		return nil
	}
	if _, err := s.recorder.RecordUnmetDomains(ctx, orderID, unmet, s.now()); err != nil {
		return fmt.Errorf("verify window: record unmet domains for order %s: %w", orderID, err)
	}
	return nil
}

// publishRestoreAlerts 窗口关闭恢复常规告警（AC-3/AC-4）：未达标域名发布
// change_linked 事件且 VerifyWindow.Active=false——4.3 路由判定恢复常规通道
// （"窗口关闭后恢复常规 diff 判定"），域名转常规 TLS 差异持续跟踪并记入未达标清单。
func (s *verifyWindowService) publishRestoreAlerts(
	ctx context.Context,
	order domain.ChangeOrder,
	expected *domain.VerifyExpected,
	judge verifyJudge,
	confirmProbes int,
) error {
	for _, tgt := range judge.Targets {
		if tgt.Met {
			continue
		}
		at := s.now()
		if err := s.publisher.PublishAlert(ctx, CertAlertEvent{
			Category:    AlertCategoryChangeLinked,
			Title:       "验证窗口关闭仍未达标，恢复常规差异跟踪",
			Fingerprint: order.OldCertFingerprint,
			Domain:      tgt.Domain,
			OrderID:     order.ID.Hex(),
			Detail: fmt.Sprintf("域名 %s 窗口到期未达预期指纹（连续一致 %d/%d 次），恢复常规 diff 判定",
				tgt.Judged, tgt.Streak, confirmProbes),
			At: at,
			VerifyWindow: &VerifyWindowContext{
				Active:              false,
				OrderID:             order.ID.Hex(),
				ExpectedFingerprint: expected.NewCertFingerprint,
				PassCount:           tgt.Streak,
			},
		}); err != nil {
			return fmt.Errorf("verify window: publish restore alert for domain %s: %w", tgt.Domain, err)
		}
	}
	return nil
}

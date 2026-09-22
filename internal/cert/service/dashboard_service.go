package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------------------------------------------------------------------
// 到期看板服务（api-handbook 到期看板端点；证书粒度主表 + 服务端孤儿隐藏）
// ---------------------------------------------------------------------

// LevelCounts 单档双口径计数（total = visible + hidden，双口径闭合）。
type LevelCounts struct {
	Total   int // 全量（含被隐藏孤儿）
	Visible int // 默认视图可见行数
	Hidden  int // 被「过期且 no_refs_scanned 且快照新鲜」谓词隐藏的行数
}

// DashboardLevelCounts 5 个互斥到期分桶（UI 总览卡序），每档双口径：
// [0]=gt30 >30 天、[1]=le30（14,30]、[2]=le14（7,14]、[3]=le7（0,7]、[4]=expired 已过期。
// 桶口径按证书粒度互斥划分（与台账列表 daysLeft 筛选分档对齐）。
type DashboardLevelCounts [5]LevelCounts

// 桶下标（countsByLevel 数组序，与前端 5 张总览卡一一对应）。
const (
	levelIdxGT30 = iota
	levelIdxLE30
	levelIdxLE14
	levelIdxLE7
	levelIdxExpired
)

// levelIdxByTier 分级标签 → countsByLevel 数组下标。
var levelIdxByTier = map[DaysLeftTier]int{
	DaysLeftGT30:    levelIdxGT30,
	DaysLeftLE30:    levelIdxLE30,
	DaysLeftLE14:    levelIdxLE14,
	DaysLeftLE7:     levelIdxLE7,
	DaysLeftExpired: levelIdxExpired,
}

// DashboardSummary 看板汇总卡（5 分级卡 + 2 计数卡 + 3 覆盖率卡）。
type DashboardSummary struct {
	CountsByLevel        DashboardLevelCounts
	HiddenCount          int // 全局被隐藏孤儿数（与 items 同一快照计算）
	DiffAlertCount       int // 最新探测 status=diff 的域名数（常规差异；不含 change_linked_diff/wildcard/unreachable/exempt）
	ExemptCount          int // 探测豁免清单条目数
	WildcardSkippedCount int // 无 override 的通配符 SAN 数（按 domain 去重；跳过拨测，探测覆盖显式缺口）
	RegistrationRate     float64
	ReplaceableRate      float64
	FingerprintOnlyRate  float64
}

// DashboardItem 看板证书行（证书粒度：每证一行，同域多证并存不掩盖）。
// CertID/Fingerprint/LastProbeAt/OnlineFingerprint 为探测详情抽屉数据；
// ProbeStatus 为行内聚合徽标（该证全部 SAN 探测态按最差优先序收敛，
// 空串=未探测）；ReferenceStatus 行级下发（台账三态判定单点派生）；
// LastScanAt 为三态判定所用扫描快照时点（新鲜度降级时展示）。
type DashboardItem struct {
	CertID            string       // 证书 ID（行 key + 抽屉「查看证书详情」跳转 /certs/:id）
	Fingerprint       string       // 台账指纹（线上指纹比对基准）
	CommonName        string       // 证书 CN（主域名仅作字段，不再是行主体）
	Sans              []string     // 全部 SAN（行内徽标 tooltip/抽屉多 SAN 视图数据源）
	Issuer            string       // 签发者
	DaysLeft          int          // 剩余天数（floor 口径，同台账列表；已过期为负）
	Level             DaysLeftTier // 互斥分桶
	HostingType       domain.HostingStatus
	ReferenceStatus   domain.ReferenceStatus // 三态（has_refs/no_refs_scanned/blind_spot；台账单点派生）
	ReferencedClouds  []string               // 引用资源所属云去重集合（K8s 引用记 "k8s"）
	ProbeStatus       domain.ProbeStatus     // 行内聚合徽标（最差优先；空串=未探测）
	LastProbeAt       *time.Time             // SAN 内最近探测时点；未探测为 nil
	OnlineFingerprint string                 // 最差探测态 SAN 的线上指纹；无值场景为空串
	LastScanAt        *time.Time             // 三态判定所用快照 startedAt；无快照为 nil
	Hidden            bool                   // 服务端隐藏谓词命中（includeHidden=true 时随行下发）
}

// DashboardView GET /dashboard 响应载荷。
type DashboardView struct {
	Summary          DashboardSummary
	Items            []DashboardItem // 证书行，按 notAfter asc（并列指纹字典序）
	LastInspectionAt *time.Time      // 最近巡检时点（4.4 记录；未接线为 nil → null）
}

// LastInspectionSource 最近巡检时点来源端口（4.4 巡检任务记录 lastInspectionAt，
// "供 dashboard 展示"；4.5 仅消费）。nil 时看板该字段输出 null。
type LastInspectionSource interface {
	// LastInspectionAt 返回最近巡检时点；ok=false 表示尚无巡检记录。
	LastInspectionAt(ctx context.Context) (at time.Time, ok bool, err error)
}

// DashboardService 到期看板（全角色含只读）：summary 实时聚合（三个 rate 字段
// 口径同 2.3 stats，经 LedgerService.Stats 复用，无存储快照）+ items 证书行。
type DashboardService interface {
	// Dashboard 聚合看板视图（Hard Rule：响应不含任何私钥/凭证字段）。
	// includeHidden：true=返回全部证书行（含被隐藏孤儿，hidden=true 标注）；
	// false=服务端孤儿隐藏生效（风险维度卡激活豁免由调用方翻译为本参数）。
	Dashboard(ctx context.Context, includeHidden bool) (DashboardView, error)
	// ListProbeResults 列出每域最近一次探测结果（LatestPerDomain），含 DNS 源探测
	// 的子域名行（tenantId/linkedResource）——供「子域名探测结果」列表视图消费。
	ListProbeResults(ctx context.Context) ([]domain.ProbeResult, error)
}

type dashboardService struct {
	certs          domain.CertificateRepository
	refs           domain.CertReferenceRepository
	snapshots      domain.ScanSnapshotRepository
	probes         domain.ProbeResultRepository
	exempts        domain.ExemptionRepository
	alertCfg       domain.AlertConfigRepository
	stats          LedgerService
	lastInspection LastInspectionSource // 可空
}

// NewDashboardService 创建看板服务。deps 说明：
//   - certs：台账（items 证书行来源=全部台账证书，每证一行）
//   - refs/snapshots：referencedClouds + 三态判定（最新成功快照单点）
//   - probes：LatestPerDomain（items 徽标聚合与 diffAlertCount 数据源）
//   - exempts：exemptCount
//   - alertCfg：wildcardProbeOverrides + scanFreshnessHours 阈值
//   - stats：三个 rate 字段（口径同 GET /stats，Hard Rule 不另算）
//   - lastInspection：lastInspectionAt 来源（4.4 接线；nil 输出 null）
func NewDashboardService(
	certs domain.CertificateRepository,
	refs domain.CertReferenceRepository,
	snapshots domain.ScanSnapshotRepository,
	probes domain.ProbeResultRepository,
	exempts domain.ExemptionRepository,
	alertCfg domain.AlertConfigRepository,
	stats LedgerService,
	lastInspection LastInspectionSource,
) DashboardService {
	return &dashboardService{
		certs:          certs,
		refs:           refs,
		snapshots:      snapshots,
		probes:         probes,
		exempts:        exempts,
		alertCfg:       alertCfg,
		stats:          stats,
		lastInspection: lastInspection,
	}
}

// Dashboard 看板聚合：单次拉取台账/探测/豁免/配置/快照上下文后内存收敛。
// countsByLevel 与 items 同一快照内收敛（SC 同快照断言）；孤儿隐藏复用台账
// 三态判定单点（deriveRefStatusFor）+ 扫描新鲜度阈值（scanSnapshotFresh），
// 看板侧不另写判定。
func (s *dashboardService) Dashboard(ctx context.Context, includeHidden bool) (DashboardView, error) {
	certs, err := s.certs.ListSummaries(ctx)
	if err != nil {
		return DashboardView{}, fmt.Errorf("dashboard: list ledger certificates: %w", err)
	}
	st, err := s.stats.Stats(ctx)
	if err != nil {
		return DashboardView{}, fmt.Errorf("dashboard: ledger stats: %w", err)
	}
	exemptions, err := s.exempts.List(ctx)
	if err != nil {
		return DashboardView{}, fmt.Errorf("dashboard: list exemptions: %w", err)
	}
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return DashboardView{}, fmt.Errorf("dashboard: get alert config: %w", err)
	}
	latestProbes, err := s.probes.LatestPerDomain(ctx)
	if err != nil {
		return DashboardView{}, fmt.Errorf("dashboard: latest probe results: %w", err)
	}
	snap, snapRefs, found, err := s.latestDoneSnapshot(ctx)
	if err != nil {
		return DashboardView{}, err
	}

	now := time.Now()
	view := DashboardView{Items: []DashboardItem{}}
	view.Summary.RegistrationRate = st.RegistrationRate
	view.Summary.ReplaceableRate = st.ReplaceableRate
	view.Summary.FingerprintOnlyRate = st.FingerprintOnlyRate
	view.Summary.ExemptCount = len(exemptions)

	// diffAlertCount：最新探测为常规 diff 的域名数（change_linked_diff 为窗口内
	// 预期切换、wildcard/unreachable/exempt 均不计差异告警）
	probeByDomain := make(map[string]domain.ProbeResult, len(latestProbes))
	for _, p := range latestProbes {
		probeByDomain[p.Domain] = p
		if p.Status == domain.ProbeStatusDiff {
			view.Summary.DiffAlertCount++
		}
	}

	// 三态判定上下文单点：最新成功快照 + 快照引用一次拉取（批量内存归约，
	// 禁逐证 N+1 重拉快照）；referencedClouds 与三态共用同一快照。
	clouds := map[string]map[string]bool{}
	var lastScanAt *time.Time
	snapFresh := false // 无快照=引用状态未知（blind_spot）→ 不隐藏
	if found {
		clouds = cloudsFromRefs(snapRefs)
		at := snap.StartedAt
		lastScanAt = &at
		snapFresh = scanSnapshotFresh(&snap, now, cfg.Thresholds.ScanFreshnessHours)
	}
	snapPtr := &snap
	if !found {
		snapPtr = nil
	}

	// wildcardSkippedCount 按 domain 去重（共享通配符 SAN 的多证不重复计数）
	wildcardCounted := make(map[string]bool)

	// items：台账全部证书逐证成行；三态仅对「快照计数=0」候选走
	// deriveRefStatusFor（计数>0 纯内存 has_refs；无快照 blind_spot），
	// 历史引用惰性拉取与台账隐藏路径同构。
	type row struct {
		item     DashboardItem
		notAfter time.Time
	}
	rows := make([]row, 0, len(certs))
	for _, c := range certs {
		refSt, err := deriveRefStatusFor(c.Fingerprint, snapPtr, snapRefs, func() ([]domain.CertReference, error) {
			return s.refs.ListByFingerprint(ctx, c.Fingerprint)
		})
		if err != nil {
			return DashboardView{}, fmt.Errorf("dashboard: derive reference status %s: %w", c.Fingerprint, err)
		}
		expired := isExpired(c.NotAfter, now)
		hidden := shouldHideExpiredNoRefs(refSt, expired, snapFresh)

		level := levelOf(c.NotAfter, now)
		lc := &view.Summary.CountsByLevel[levelIdxByTier[level]]
		lc.Total++
		if hidden {
			lc.Hidden++
			view.Summary.HiddenCount++
		} else {
			lc.Visible++
		}

		sans := make([]string, 0, len(c.Sans))
		for _, san := range c.Sans {
			if name := strings.TrimSpace(san); name != "" {
				sans = append(sans, name)
			}
		}
		// 通配符 SAN 无 override → 跳过拨测（计数可见、不计差异、不告警；
		// 按 domain 去重）
		for _, name := range sans {
			if isWildcardSAN(name) && !wildcardCounted[name] {
				if _, overridden := cfg.WildcardProbeOverrides[name]; !overridden {
					view.Summary.WildcardSkippedCount++
					wildcardCounted[name] = true
				}
			}
		}

		probeStatus, lastProbeAt, onlineFP := aggregateProbeBadge(sans, probeByDomain)
		if includeHidden || !hidden {
			rows = append(rows, row{
				item: DashboardItem{
					CertID:            c.ID.Hex(),
					Fingerprint:       c.Fingerprint,
					CommonName:        c.CommonName,
					Sans:              sans,
					Issuer:            c.Issuer,
					DaysLeft:          daysLeft(c.NotAfter, now),
					Level:             level,
					HostingType:       c.HostingStatus,
					ReferenceStatus:   refSt.Status,
					ReferencedClouds:  sortedClouds(clouds[c.Fingerprint]),
					ProbeStatus:       probeStatus,
					LastProbeAt:       lastProbeAt,
					OnlineFingerprint: onlineFP,
					LastScanAt:        lastScanAt,
					Hidden:            hidden,
				},
				notAfter: c.NotAfter,
			})
		}
	}

	// 排序：notAfter asc（最快到期优先），并列按证书指纹字典序稳定取序
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].notAfter.Equal(rows[j].notAfter) {
			return rows[i].notAfter.Before(rows[j].notAfter)
		}
		return rows[i].item.Fingerprint < rows[j].item.Fingerprint
	})
	view.Items = make([]DashboardItem, 0, len(rows))
	for _, r := range rows {
		view.Items = append(view.Items, r.item)
	}

	if s.lastInspection != nil {
		at, ok, err := s.lastInspection.LastInspectionAt(ctx)
		if err != nil {
			return DashboardView{}, fmt.Errorf("dashboard: last inspection time: %w", err)
		}
		if ok {
			view.LastInspectionAt = &at
		}
	}
	return view, nil
}

// ListProbeResults 每域最近一次探测结果（LatestPerDomain，含 DNS 源子域名行）。
func (s *dashboardService) ListProbeResults(ctx context.Context) ([]domain.ProbeResult, error) {
	return s.probes.LatestPerDomain(ctx)
}

// latestDoneSnapshot 最新成功快照及其全部引用（found=false 表示无成功快照——
// 三态派生一律 blind_spot，引用状态未知保守显示）。
func (s *dashboardService) latestDoneSnapshot(ctx context.Context) (domain.ScanSnapshot, []domain.CertReference, bool, error) {
	snap, err := s.snapshots.LatestDone(ctx)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return domain.ScanSnapshot{}, nil, false, nil
	}
	if err != nil {
		return domain.ScanSnapshot{}, nil, false, fmt.Errorf("dashboard: latest done snapshot: %w", err)
	}
	refs, err := s.refs.ListBySnapshotID(ctx, snap.ID.Hex())
	if err != nil {
		return domain.ScanSnapshot{}, nil, false, fmt.Errorf("dashboard: snapshot references: %w", err)
	}
	return snap, refs, true, nil
}

// cloudsFromRefs 快照引用 → 指纹→所属云去重集合（K8s 引用 cloud 为空，记
// "k8s"，与看板云 chips 语义对齐）。
func cloudsFromRefs(refs []domain.CertReference) map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	for _, r := range refs {
		cloud := string(r.Cloud)
		if cloud == "" {
			cloud = "k8s"
		}
		if out[r.CertFingerprint] == nil {
			out[r.CertFingerprint] = make(map[string]bool)
		}
		out[r.CertFingerprint][cloud] = true
	}
	return out
}

// probeBadgeRank 探测徽标最差优先序（小=最差；未探测排序尾）。
var probeBadgeRank = map[domain.ProbeStatus]int{
	domain.ProbeStatusDiff:             0,
	domain.ProbeStatusChangeLinkedDiff: 1,
	domain.ProbeStatusUnreachable:      2,
	domain.ProbeStatusWildcardSkipped:  3,
	domain.ProbeStatusExempt:           4,
	domain.ProbeStatusConsistent:       5,
}

// probeRankUnprobed 未探测 SAN 的排序尾位（劣于全部已探测态）。
const probeRankUnprobed = 6

// aggregateProbeBadge 证书全部 SAN 探测态行内聚合：按最差优先序收敛（并列同态
// 不细分，同态并列取字典序最小 SAN 保证确定性）；lastProbeAt 取 SAN 内最近
// 探测时点；onlineFingerprint 取最差探测态 SAN 的线上指纹。全部未探测返回空串。
func aggregateProbeBadge(sans []string, probeByDomain map[string]domain.ProbeResult) (domain.ProbeStatus, *time.Time, string) {
	worstRank := probeRankUnprobed + 1
	var worstSAN string
	probed := false
	var lastProbeAt *time.Time
	for _, name := range sans { // sans 已按证书原序；确定性由 rank+tie 规则保证
		p, ok := probeByDomain[name]
		if !ok {
			continue // 未探测 SAN 排序尾（不参与聚合）
		}
		probed = true
		at := p.ProbeAt
		if lastProbeAt == nil || at.After(*lastProbeAt) {
			latest := at
			lastProbeAt = &latest
		}
		rank, known := probeBadgeRank[p.Status]
		if !known {
			rank = probeRankUnprobed
		}
		// 并列同态不细分：仅在严格更差时更新（同态保留先到的 SAN）
		if rank < worstRank || (rank == worstRank && name < worstSAN) {
			worstRank = rank
			worstSAN = name
		}
	}
	if !probed {
		return "", nil, ""
	}
	p := probeByDomain[worstSAN]
	return p.Status, lastProbeAt, p.OnlineFingerprint
}

// levelOf notAfter → 互斥分桶（floor daysLeft 口径，与台账 daysLeft 筛选分档一致）。
func levelOf(notAfter, now time.Time) DaysLeftTier {
	if !notAfter.After(now) {
		return DaysLeftExpired
	}
	d := daysLeft(notAfter, now)
	switch {
	case d <= 7:
		return DaysLeftLE7
	case d <= 14:
		return DaysLeftLE14
	case d <= 30:
		return DaysLeftLE30
	default:
		return DaysLeftGT30
	}
}

// sortedClouds 云集合 → 字典序切片（空集输出 [] 而非 null）。
func sortedClouds(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for cloud := range set {
		out = append(out, cloud)
	}
	sort.Strings(out)
	return out
}

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
// 到期看板服务（任务 4.5，api-handbook 到期看板端点）
// ---------------------------------------------------------------------

// DashboardLevelCounts 5 个互斥到期分桶（UI 总览卡序）：
// [0]=gt30 >30 天、[1]=le30（14,30]、[2]=le14（7,14]、[3]=le7（0,7]、[4]=expired 已过期。
// 桶口径按证书粒度互斥划分（与台账列表 daysLeft 筛选分档对齐）。
type DashboardLevelCounts [5]int

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
	DiffAlertCount       int // 最新探测 status=diff 的域名数（常规差异；不含 change_linked_diff/wildcard/unreachable/exempt）
	ExemptCount          int // 探测豁免清单条目数
	WildcardSkippedCount int // 无 override 的通配符 SAN 数（跳过拨测，探测覆盖显式缺口）
	RegistrationRate     float64
	ReplaceableRate      float64
	FingerprintOnlyRate  float64
}

// DashboardItem 看板子域名行。CertID/Fingerprint/LastProbeAt/OnlineFingerprint
// 为探测详情抽屉数据（任务 6.4：ui-design Data Binding「探测详情抽屉」行——
// lastProbeAt/onlineFingerprint 来自 TLS 探测记录，certId/fingerprint 供
// 「查看证书详情」链接与线上/台账指纹比对；未探测时探测字段为零值）。
type DashboardItem struct {
	Domain            string
	DaysLeft          int // 归属证书剩余天数（floor 口径，同台账列表）
	Level             DaysLeftTier
	HostingType       domain.HostingStatus
	ProbeStatus       domain.ProbeStatus // 空串=尚未探测
	ReferencedClouds  []string           // 归属证书引用资源所属云去重集合（K8s 引用记 "k8s"）
	CertID            string             // 归属证书 ID（抽屉「查看证书详情」跳转 /certs/:id）
	Fingerprint       string             // 归属证书台账指纹（线上指纹比对基准）
	LastProbeAt       *time.Time         // 最近探测时点；未探测为 nil
	OnlineFingerprint string             // 线上生效证书指纹；unreachable/wildcard_skipped 等无值场景为空串
}

// DashboardView GET /dashboard 响应载荷。
type DashboardView struct {
	Summary          DashboardSummary
	Items            []DashboardItem // 按 domain 字典序
	LastInspectionAt *time.Time      // 最近巡检时点（4.4 记录；未接线为 nil → null）
}

// LastInspectionSource 最近巡检时点来源端口（4.4 巡检任务记录 lastInspectionAt，
// "供 dashboard 展示"；4.5 仅消费）。nil 时看板该字段输出 null。
type LastInspectionSource interface {
	// LastInspectionAt 返回最近巡检时点；ok=false 表示尚无巡检记录。
	LastInspectionAt(ctx context.Context) (at time.Time, ok bool, err error)
}

// DashboardService 到期看板（全角色含只读）：summary 实时聚合（三个 rate 字段
// 口径同 2.3 stats，经 LedgerService.Stats 复用，无存储快照）+ items 子域名行。
type DashboardService interface {
	// Dashboard 聚合看板视图（Hard Rule：响应不含任何私钥/凭证字段）。
	Dashboard(ctx context.Context) (DashboardView, error)
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
//   - certs：台账（items 域名来源=全部 sans 展开去重）
//   - refs/snapshots：referencedClouds（最新成功快照 CertReference 所属云去重）
//   - probes：LatestPerDomain（items.probeStatus 与 diffAlertCount 数据源）
//   - exempts：exemptCount
//   - alertCfg：wildcardProbeOverrides（wildcardSkippedCount 判定）
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

// Dashboard 看板聚合：单次拉取台账/探测/豁免/配置/引用上下文后内存收敛。
func (s *dashboardService) Dashboard(ctx context.Context) (DashboardView, error) {
	certs, err := s.certs.List(ctx)
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
	cloudsByFP, err := s.cloudsByFingerprint(ctx)
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

	// items：台账全部 sans 展开去重；同域名多证书并存时归属 notAfter 最新证书
	//（线上更可能生效；notAfter 相同按指纹字典序稳定取一）
	owner := make(map[string]domain.Certificate)
	for _, c := range certs {
		view.Summary.CountsByLevel[levelIdxByTier[levelOf(c.NotAfter, now)]]++
		for _, san := range c.Sans {
			name := strings.TrimSpace(san)
			if name == "" {
				continue
			}
			cur, exists := owner[name]
			if !exists || preferOwner(c, cur) {
				owner[name] = c
			}
		}
	}
	domains := make([]string, 0, len(owner))
	for name := range owner {
		domains = append(domains, name)
	}
	sort.Strings(domains)

	for _, name := range domains {
		c := owner[name]
		item := DashboardItem{
			Domain:           name,
			DaysLeft:         daysLeft(c.NotAfter, now),
			Level:            levelOf(c.NotAfter, now),
			HostingType:      c.HostingStatus,
			ReferencedClouds: sortedClouds(cloudsByFP[c.Fingerprint]),
			CertID:           c.ID.Hex(),
			Fingerprint:      c.Fingerprint,
		}
		if p, ok := probeByDomain[name]; ok {
			item.ProbeStatus = p.Status
			probeAt := p.ProbeAt
			item.LastProbeAt = &probeAt
			item.OnlineFingerprint = p.OnlineFingerprint
		}
		// 通配符 SAN 无 override → 跳过拨测（计数可见、不计差异、不告警）
		if isWildcardSAN(name) {
			if _, overridden := cfg.WildcardProbeOverrides[name]; !overridden {
				view.Summary.WildcardSkippedCount++
			}
		}
		view.Items = append(view.Items, item)
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

// cloudsByFingerprint 最新成功快照引用 → 指纹→所属云去重集合
// （K8s 引用 cloud 为空，记 "k8s"，与看板云 chips 语义对齐）。
func (s *dashboardService) cloudsByFingerprint(ctx context.Context) (map[string]map[string]bool, error) {
	snap, err := s.snapshots.LatestDone(ctx)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return map[string]map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dashboard: latest done snapshot: %w", err)
	}
	refs, err := s.refs.ListBySnapshotID(ctx, snap.ID.Hex())
	if err != nil {
		return nil, fmt.Errorf("dashboard: snapshot references: %w", err)
	}
	out := make(map[string]map[string]bool)
	for _, r := range refs {
		cloud := string(r.Cloud)
		if cloud == "" {
			cloud = "k8s" // K8s 引用（product=crd）无云归属，以通道名呈现
		}
		if out[r.CertFingerprint] == nil {
			out[r.CertFingerprint] = make(map[string]bool)
		}
		out[r.CertFingerprint][cloud] = true
	}
	return out, nil
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

// preferOwner 同域名多证书归属判定：notAfter 更新者胜（线上更可能生效），
// 相同按指纹字典序（确定性）。
func preferOwner(cand, cur domain.Certificate) bool {
	if !cand.NotAfter.Equal(cur.NotAfter) {
		return cand.NotAfter.After(cur.NotAfter)
	}
	return cand.Fingerprint < cur.Fingerprint
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

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

// CoverageBoundaryDeclaration 覆盖边界声明（PRD Story 2 / 任务 3.6 Hard Rule）：
// 引用视图固定标注的覆盖边界文案，正向响应不可省略。
const CoverageBoundaryDeclaration = "本视图不含 VM Nginx 配置级引用"

// DenominatorUnavailableNote total=-1 时的"分母不可用"标记（api-handbook
// CoverageMeta：total=-1 表示分母不可用，输出盲区声明而非 0%）。
const DenominatorUnavailableNote = "分母不可用（覆盖率盲区）"

// ---------------------------------------------------------------------
// 共享引用三态派生（抽自 2.3 ledgerService，避免复制）
// ---------------------------------------------------------------------

// refStatusView 引用三态派生结果（2.3 台账详情/删除拦截与 3.6 引用视图共用）。
type refStatusView struct {
	Status   domain.ReferenceStatus
	RefCount int    // has_refs 时为最新成功快照引用计数
	Reason   string // blind_spot/保护期原因；has_refs 时为计数描述
}

// deriveRefStatusFor 引用三态派生核心（tech-design"引用状态语义"，
// "未发现引用" ≠ "无引用"）：
//   - snapshot=nil → blind_spot（无成功快照）；
//   - 最新成功快照中该指纹计数 > 0 → has_refs；
//   - 计数=0 且该证书涉及云/产品均在快照范围内 → no_refs_scanned
//     （已扫描无匹配，可能因权限不足/产品未覆盖漏报）；
//   - 计数=0 且存在范围外云/产品 → blind_spot（附未覆盖清单）。
//
// snapshotRefs 为最新成功快照全部引用；fetchHistory 惰性拉取该指纹跨快照
// 累计引用（仅计数=0 时调用，供"涉及云/产品"判定），nil 视为空历史。
func deriveRefStatusFor(
	fingerprint string,
	snapshot *domain.ScanSnapshot,
	snapshotRefs []domain.CertReference,
	fetchHistory func() ([]domain.CertReference, error),
) (refStatusView, error) {
	if snapshot == nil {
		return refStatusView{
			Status: domain.RefStatusBlindSpot,
			Reason: "no successful scan snapshot",
		}, nil
	}
	count := 0
	for _, r := range snapshotRefs {
		if r.CertFingerprint == fingerprint {
			count++
		}
	}
	if count > 0 {
		return refStatusView{
			Status:   domain.RefStatusHasRefs,
			RefCount: count,
			Reason:   fmt.Sprintf("referenced by %d resource(s) in latest successful scan", count),
		}, nil
	}

	// 计数=0：校验该证书涉及云/产品是否都在最新成功快照范围内
	var history []domain.CertReference
	if fetchHistory != nil {
		var err error
		history, err = fetchHistory()
		if err != nil {
			return refStatusView{}, err
		}
	}
	covered := make(map[string]bool, len(snapshot.CoverageMeta))
	for _, cm := range snapshot.CoverageMeta {
		covered[cm.Cloud+"/"+cm.Product] = true
	}
	var uncovered []string
	seen := make(map[string]bool)
	for _, r := range history {
		if r.Cloud == "" || r.Product == "" {
			continue
		}
		key := string(r.Cloud) + "/" + string(r.Product)
		if seen[key] {
			continue
		}
		seen[key] = true
		if !covered[key] {
			uncovered = append(uncovered, key)
		}
	}
	if len(uncovered) > 0 {
		sort.Strings(uncovered)
		return refStatusView{
			Status: domain.RefStatusBlindSpot,
			Reason: "scan scope did not cover: " + strings.Join(uncovered, ", "),
		}, nil
	}
	return refStatusView{Status: domain.RefStatusNoRefsScanned}, nil
}

// ScanInProgressError 409 SCAN_IN_PROGRESS（防重触发），附进行中快照信息
// （snapshotId/startedAt，tech-design 同步错误语境）。包装 domain.ErrScanInProgress。
type ScanInProgressError struct {
	SnapshotID string
	StartedAt  time.Time
}

// Error 实现 error；文案与 domain.ErrScanInProgress 一致（不泄露内部细节）。
func (e *ScanInProgressError) Error() string { return domain.ErrScanInProgress.Error() }

// Unwrap 保持 errors.Is(err, domain.ErrScanInProgress) 可判定。
func (e *ScanInProgressError) Unwrap() error { return domain.ErrScanInProgress }

// ---------------------------------------------------------------------
// 引用查询服务（任务 3.6）
// ---------------------------------------------------------------------

// ScanTriggerPort 立即扫描触发端口（3.5 ReferenceScanService.StartScan 的窄化
// 依赖，测试注入 fake）。
type ScanTriggerPort interface {
	StartScan(ctx context.Context) (ScanResult, error)
	StartScanAsync(ctx context.Context) (ScanResult, error)
}

// ReferenceItem 正向视图单条引用（AC 白名单字段）。
type ReferenceItem struct {
	ResourceID            string
	ReferencedCloudCertID string
	AccountKey            string
	Namespace             string // K8s 引用
	Kind                  string // K8s 引用
}

// ReferenceGroup 按云/产品/集群分组的引用集合。
type ReferenceGroup struct {
	Cloud      string
	Product    string
	ClusterID  string // K8s 分组键；云分组为空
	References []ReferenceItem
}

// ReferenceView 正向引用视图（GET /:id/references 服务结果）。
type ReferenceView struct {
	CertID           string
	Fingerprint      string
	ReferenceStatus  domain.ReferenceStatus
	RefCount         int
	Reason           string // blind_spot 原因 / has_refs 计数描述
	LastScanAt       *time.Time
	SnapshotID       string
	Coverage         []domain.CoverageMeta
	Groups           []ReferenceGroup
	CoverageBoundary string // 覆盖边界声明（Hard Rule：不可省略）
}

// ReverseReference 反向查询单条引用（定位字段内联，按指纹归属其证书条目）。
type ReverseReference struct {
	Cloud                 string
	Product               string
	ClusterID             string
	Namespace             string
	Kind                  string
	ResourceID            string
	ReferencedCloudCertID string
	AccountKey            string
}

// ReverseCertEntry 反向查询单证书条目（Hard Rule：按指纹严格区分，不做同域名合并）。
type ReverseCertEntry struct {
	Fingerprint    string
	CertID         string // 台账登记证书 ID；未登记为空
	Registered     bool
	CommonName     string // 未登记为空
	Sans           []string
	HostingStatus  domain.HostingStatus // 未登记为空
	ReferenceCount int
	References     []ReverseReference
}

// ReferenceQueryService 引用关系读取端点服务（任务 3.6）：正向分组视图、
// 反向查询、立即扫描触发（防重）。
type ReferenceQueryService interface {
	// References 正向引用视图：分组引用 + 扫描元数据（lastScanAt/coverage）+
	// referenceStatus + 盲区声明。
	References(ctx context.Context, certID string) (ReferenceView, error)
	// ReverseQuery 按域名/资源名反查引用证书列表（按指纹分组）；无匹配返回空列表。
	ReverseQuery(ctx context.Context, query string) ([]ReverseCertEntry, error)
	// TriggerScan 触发 3.5 扫描任务；进行中返回 *ScanInProgressError（409）。
	TriggerScan(ctx context.Context, certID string) (ScanResult, error)
}

type referenceQueryService struct {
	certs     domain.CertificateRepository
	refs      domain.CertReferenceRepository
	snapshots domain.ScanSnapshotRepository
	scan      ScanTriggerPort
}

// NewReferenceQueryService 创建引用查询服务。
func NewReferenceQueryService(
	certs domain.CertificateRepository,
	refs domain.CertReferenceRepository,
	snapshots domain.ScanSnapshotRepository,
	scan ScanTriggerPort,
) ReferenceQueryService {
	return &referenceQueryService{certs: certs, refs: refs, snapshots: snapshots, scan: scan}
}

// ---------------------------------------------------------------------
// 正向引用视图
// ---------------------------------------------------------------------

// References 正向视图编排：证书定位 → 最新成功快照（无快照=盲区）→ 三态派生
// （共享 2.3 helper）→ 分组收敛（云/产品/集群三键，字典序稳定）。
func (s *referenceQueryService) References(ctx context.Context, certID string) (ReferenceView, error) {
	cert, err := s.certs.GetByID(ctx, certID)
	if err != nil {
		return ReferenceView{}, err // ErrInvalidID / mongo.ErrNoDocuments
	}
	view := ReferenceView{
		CertID:           cert.ID.Hex(),
		Fingerprint:      cert.Fingerprint,
		Groups:           []ReferenceGroup{},
		Coverage:         []domain.CoverageMeta{},
		CoverageBoundary: CoverageBoundaryDeclaration,
	}

	snap, err := s.snapshots.LatestDone(ctx)
	if errors.Is(err, mongo.ErrNoDocuments) {
		st, err := deriveRefStatusFor(cert.Fingerprint, nil, nil, nil)
		if err != nil {
			return ReferenceView{}, err
		}
		return applyStatus(view, st), nil
	}
	if err != nil {
		return ReferenceView{}, err
	}
	refs, err := s.refs.ListBySnapshotID(ctx, snap.ID.Hex())
	if err != nil {
		return ReferenceView{}, err
	}
	st, err := deriveRefStatusFor(cert.Fingerprint, &snap, refs, func() ([]domain.CertReference, error) {
		return s.refs.ListByFingerprint(ctx, cert.Fingerprint)
	})
	if err != nil {
		return ReferenceView{}, err
	}
	view = applyStatus(view, st)
	view.LastScanAt = &snap.StartedAt
	view.SnapshotID = snap.ID.Hex()
	view.Coverage = append(view.Coverage, snap.CoverageMeta...)
	view.Groups = groupReferences(refs, cert.Fingerprint)
	return view, nil
}

// applyStatus 三态派生结果写入视图（保持值语义，返回新副本）。
func applyStatus(v ReferenceView, st refStatusView) ReferenceView {
	v.ReferenceStatus = st.Status
	v.RefCount = st.RefCount
	v.Reason = st.Reason
	return v
}

// groupReferences 快照引用 → 该指纹的云/产品/集群分组（组间与组内均字典序稳定）。
func groupReferences(refs []domain.CertReference, fingerprint string) []ReferenceGroup {
	type groupKey struct{ cloud, product, cluster string }
	order := make([]groupKey, 0, 4)
	byKey := make(map[groupKey][]ReferenceItem, 4)
	for _, r := range refs {
		if r.CertFingerprint != fingerprint {
			continue
		}
		k := groupKey{cloud: string(r.Cloud), product: string(r.Product), cluster: r.ClusterID}
		if _, exists := byKey[k]; !exists {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], ReferenceItem{
			ResourceID:            r.ResourceID,
			ReferencedCloudCertID: r.ReferencedCloudCertID,
			AccountKey:            r.AccountKey,
			Namespace:             r.Namespace,
			Kind:                  r.Kind,
		})
	}
	sort.Slice(order, func(i, j int) bool {
		// 云分组在前（cloud 字典序），K8s 分组（cloud 空）在后（product/cluster 字典序）
		if (order[i].cloud == "") != (order[j].cloud == "") {
			return order[i].cloud != ""
		}
		if order[i].cloud != order[j].cloud {
			return order[i].cloud < order[j].cloud
		}
		if order[i].product != order[j].product {
			return order[i].product < order[j].product
		}
		return order[i].cluster < order[j].cluster
	})
	sortRefs := func(items []ReferenceItem) {
		sort.Slice(items, func(i, j int) bool {
			if items[i].ResourceID != items[j].ResourceID {
				return items[i].ResourceID < items[j].ResourceID
			}
			return items[i].ReferencedCloudCertID < items[j].ReferencedCloudCertID
		})
	}
	out := make([]ReferenceGroup, 0, len(order))
	for _, k := range order {
		items := byKey[k]
		sortRefs(items)
		out = append(out, ReferenceGroup{
			Cloud:      k.cloud,
			Product:    k.product,
			ClusterID:  k.cluster,
			References: items,
		})
	}
	return out
}

// ---------------------------------------------------------------------
// 反向查询
// ---------------------------------------------------------------------

// ReverseQuery 域名/资源名 → 引用证书列表。匹配口径：
//   - 域名：台账证书 SAN/CommonName 精确匹配（不区分大小写），通配符 SAN
//     （*.base）覆盖单标签子域名（PRD 通配符语义）；
//   - 资源名：最新成功快照引用 resourceId 精确匹配（含未登记指纹，
//     Registered=false 呈现）。
//
// 每证书条目携带自身引用列表（Hard Rule：按指纹严格区分，不做同域名合并）；
// 无匹配返回空列表（区别于错误）。无成功快照时资源名维度自然为空。
func (s *referenceQueryService) ReverseQuery(ctx context.Context, query string) ([]ReverseCertEntry, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return []ReverseCertEntry{}, nil
	}

	var snapRefs []domain.CertReference
	if snap, err := s.snapshots.LatestDone(ctx); err == nil {
		snapRefs, err = s.refs.ListBySnapshotID(ctx, snap.ID.Hex())
		if err != nil {
			return nil, err
		}
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}

	// 资源名维度：resourceId 命中 → 该引用的指纹入候选（含未登记指纹）
	candidates := make(map[string]struct{})
	refsByFP := make(map[string][]ReverseReference)
	for _, r := range snapRefs {
		rr := toReverseReference(r)
		refsByFP[r.CertFingerprint] = append(refsByFP[r.CertFingerprint], rr)
		if strings.EqualFold(r.ResourceID, query) {
			candidates[r.CertFingerprint] = struct{}{}
		}
	}

	// 域名维度：台账证书 SAN/CN 覆盖 → 入候选
	certs, err := s.certs.List(ctx)
	if err != nil {
		return nil, err
	}
	registered := make(map[string]domain.Certificate, len(certs))
	for _, c := range certs {
		registered[c.Fingerprint] = c
		if certCoversDomain(c, q) {
			candidates[c.Fingerprint] = struct{}{}
		}
	}

	fps := make([]string, 0, len(candidates))
	for fp := range candidates {
		fps = append(fps, fp)
	}
	sort.Strings(fps)

	entries := make([]ReverseCertEntry, 0, len(fps))
	for _, fp := range fps {
		e := ReverseCertEntry{
			Fingerprint: fp,
			References:  refsByFP[fp],
		}
		if e.References == nil {
			e.References = []ReverseReference{}
		}
		e.ReferenceCount = len(e.References)
		if c, ok := registered[fp]; ok {
			e.Registered = true
			e.CertID = c.ID.Hex()
			e.CommonName = c.CommonName
			e.Sans = nonNilSans(c.Sans)
			e.HostingStatus = c.HostingStatus
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// toReverseReference 引用文档 → 反向条目引用形态。
func toReverseReference(r domain.CertReference) ReverseReference {
	return ReverseReference{
		Cloud:                 string(r.Cloud),
		Product:               string(r.Product),
		ClusterID:             r.ClusterID,
		Namespace:             r.Namespace,
		Kind:                  r.Kind,
		ResourceID:            r.ResourceID,
		ReferencedCloudCertID: r.ReferencedCloudCertID,
		AccountKey:            r.AccountKey,
	}
}

// certCoversDomain 证书是否覆盖查询域名：SAN/CommonName 精确匹配（不区分大小写）
// 或通配符 SAN 单标签覆盖（*.example.com 命中 a.example.com，不命中 a.b.example.com）。
func certCoversDomain(c domain.Certificate, lowerQuery string) bool {
	if strings.EqualFold(c.CommonName, lowerQuery) {
		return true
	}
	for _, san := range c.Sans {
		if strings.EqualFold(san, lowerQuery) {
			return true
		}
		if rest, ok := strings.CutPrefix(san, "*."); ok && rest != "" {
			// 单标签覆盖：query = <label>.<rest> 且 label 不含 "."
			tail := "." + strings.ToLower(rest)
			if strings.HasSuffix(lowerQuery, tail) {
				label := strings.TrimSuffix(lowerQuery, tail)
				if label != "" && !strings.Contains(label, ".") {
					return true
				}
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------
// 立即扫描触发（防重）
// ---------------------------------------------------------------------

// TriggerScan 触发 3.5 扫描任务：证书存在性校验 → StartScanAsync（异步，
// 同步建 running 快照后立即返回 running 态；发现+终态在后台 goroutine）；
// 进行中（domain.ErrScanInProgress）→ 附最新 running 快照信息包装为
// *ScanInProgressError（409 SCAN_IN_PROGRESS）。
func (s *referenceQueryService) TriggerScan(ctx context.Context, certID string) (ScanResult, error) {
	if _, err := s.certs.GetByID(ctx, certID); err != nil {
		return ScanResult{}, err // ErrInvalidID → 400 / ErrNoDocuments → 404
	}
	res, err := s.scan.StartScanAsync(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrScanInProgress) {
			return ScanResult{}, s.wrapInProgress(ctx)
		}
		return ScanResult{}, err
	}
	return res, nil
}

// wrapInProgress 构造附进行中快照信息的防重错误（快照读取失败不改变 409 语义，
// 仅丢失附加信息）。
func (s *referenceQueryService) wrapInProgress(ctx context.Context) error {
	e := &ScanInProgressError{}
	if snap, err := s.snapshots.LatestRunning(ctx); err == nil {
		e.SnapshotID = snap.ID.Hex()
		e.StartedAt = snap.StartedAt
	}
	return e
}

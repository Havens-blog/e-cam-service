package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"go.mongodb.org/mongo-driver/mongo"
)

// 分页常量（AC：服务端分页每页 20）。
const (
	// DefaultCertPageSize 台账列表默认每页条数。
	DefaultCertPageSize = 20
	// MaxCertPageSize 每页条数上限（防超大页拖垮实时聚合）。
	MaxCertPageSize = 100
)

// DaysLeftTier 到期分档筛选（与前端筛选器对齐：>30 / ≤30 / ≤14 / ≤7 / 已过期）。
type DaysLeftTier string

const (
	DaysLeftGT30    DaysLeftTier = "gt30"    // 剩余 >30 天
	DaysLeftLE30    DaysLeftTier = "le30"    // 未过期且剩余 ≤30 天
	DaysLeftLE14    DaysLeftTier = "le14"    // 未过期且剩余 ≤14 天
	DaysLeftLE7     DaysLeftTier = "le7"     // 未过期且剩余 ≤7 天
	DaysLeftExpired DaysLeftTier = "expired" // 已过期（notAfter ≤ now）
)

// ParseDaysLeftTier 解析 daysLeft 筛选参数；空串=不筛，第二返回值 false 表示非法值。
func ParseDaysLeftTier(s string) (DaysLeftTier, bool) {
	switch s {
	case "":
		return "", true
	case string(DaysLeftGT30), string(DaysLeftLE30), string(DaysLeftLE14),
		string(DaysLeftLE7), string(DaysLeftExpired):
		return DaysLeftTier(s), true
	}
	return "", false
}

// ListCertsQuery 台账列表查询（web 层解析后的类型化入参）。
type ListCertsQuery struct {
	Page          int
	PageSize      int                  // <=0 取默认 20，超 100 截断
	HostingStatus domain.HostingStatus // 空=不筛
	DaysLeft      DaysLeftTier         // 空=不筛
	Search        string               // 域名/SAN/指纹片段子串
}

// CertListItem 列表项（白名单字段；daysLeft/refCount 为查询时派生量）。
type CertListItem struct {
	ID            string
	Fingerprint   string
	CommonName    string
	Sans          []string
	Issuer        string
	NotAfter      time.Time
	DaysLeft      int
	HostingStatus domain.HostingStatus
	MaterialIssue domain.MaterialIssue // 盘点容忍标记（空=正常）
	ProtectUntil  *time.Time
	RefCount      int
}

// ListCertsResult 分页结果（Total 为筛选命中总数）。
type ListCertsResult struct {
	Items    []CertListItem
	Total    int64
	Page     int
	PageSize int
}

// CertDetail 详情（全要素；HasKey 布尔承载"已加密托管"语义，
// 永不携带 encryptedPrivateKey 密文/明文——Hard Rule 白名单）。
type CertDetail struct {
	ID               string
	Fingerprint      string
	CommonName       string
	Sans             []string
	Issuer           string
	SerialNumber     string
	NotBefore        time.Time
	NotAfter         time.Time
	DaysLeft         int
	KeyAlgorithm     domain.KeyAlgorithm
	HostingStatus    domain.HostingStatus
	MaterialIssue    domain.MaterialIssue // 盘点容忍标记（空=正常）
	HasKey           bool
	ExpectedDomain   string
	ProtectUntil     *time.Time
	ExpiryAlertLevel domain.ExpiryAlertLevel
	CreatedAt        time.Time
	RefCount         int
	ReferenceStatus  domain.ReferenceStatus
}

// DenominatorSources 分母构成明细（stats 响应 denominatorSources）。
type DenominatorSources struct {
	ScannedUniqueFingerprints int // 最新成功快照指纹去重数
	ManualOnlyFingerprints    int // 台账独有（未出现在最新成功快照）指纹数
}

// LedgerStats 台账统计（双口径覆盖率；Hard Rule：查询时实时聚合，不落存储快照）。
type LedgerStats struct {
	Total                int
	Complete             int
	FingerprintOnly      int
	MissingRegistrations int     // 扫描发现未登记数（登记缺口）
	RegistrationRate     float64 // 登记覆盖率 = 台账数/分母
	ReplaceableRate      float64 // 可更换托管覆盖率 = complete 数/分母
	FingerprintOnlyRate  float64 // 仅指纹登记占比 = fingerprint_only 数/台账数
	Denominator          int     // 最新成功快照指纹去重 ∪ 台账全部指纹
	DenominatorSources   DenominatorSources
}

// LedgerService 台账读取面（列表/详情/删除拦截/统计，任务 2.3）。
type LedgerService interface {
	// ListCerts 服务端分页+筛选（hostingStatus/daysLeft/search）+ refCount 派生。
	ListCerts(ctx context.Context, q ListCertsQuery) (ListCertsResult, error)
	// GetCert 全要素详情（不含私钥；HasKey 表达"已加密托管"语义）。
	GetCert(ctx context.Context, id string) (CertDetail, error)
	// DeleteCert 删除拦截：has_refs/blind_spot 一律 *domain.DeleteBlockedError；
	// 仅 no_refs_scanned 且不在保护期放行。
	DeleteCert(ctx context.Context, id string) error
	// Stats 双口径覆盖率（实时聚合，分母=扫描指纹去重∪台账全部指纹）。
	Stats(ctx context.Context) (LedgerStats, error)
}

type ledgerService struct {
	certs     domain.CertificateRepository
	refs      domain.CertReferenceRepository
	snapshots domain.ScanSnapshotRepository
}

// NewLedgerService 创建台账服务。
func NewLedgerService(
	certs domain.CertificateRepository,
	refs domain.CertReferenceRepository,
	snapshots domain.ScanSnapshotRepository,
) LedgerService {
	return &ledgerService{certs: certs, refs: refs, snapshots: snapshots}
}

// ---------------------------------------------------------------------
// 列表
// ---------------------------------------------------------------------

// ListCerts 服务端分页+筛选+search；refCount 取最新成功快照计数（无快照=0）。
func (s *ledgerService) ListCerts(ctx context.Context, q ListCertsQuery) (ListCertsResult, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	switch {
	case q.PageSize <= 0:
		q.PageSize = DefaultCertPageSize
	case q.PageSize > MaxCertPageSize:
		q.PageSize = MaxCertPageSize
	}
	if !validHostingStatus(q.HostingStatus) {
		return ListCertsResult{}, fmt.Errorf("cert: invalid hostingStatus filter %q", q.HostingStatus)
	}
	filter, err := certListFilterFor(q)
	if err != nil {
		return ListCertsResult{}, err
	}

	certs, total, err := s.certs.ListPage(ctx, filter, (q.Page-1)*q.PageSize, q.PageSize)
	if err != nil {
		return ListCertsResult{}, err
	}
	counts, err := s.referenceCounts(ctx)
	if err != nil {
		return ListCertsResult{}, err
	}

	now := time.Now()
	items := make([]CertListItem, 0, len(certs))
	for _, c := range certs {
		items = append(items, toListItem(c, counts[c.Fingerprint], now))
	}
	return ListCertsResult{Items: items, Total: total, Page: q.Page, PageSize: q.PageSize}, nil
}

// certListFilterFor 查询参数 → 仓储筛选条件。
// 到期分档换算：expired=notAfter≤now；leN=now<notAfter≤now+N 天；gt30=notAfter>now+30 天。
func certListFilterFor(q ListCertsQuery) (domain.CertListFilter, error) {
	f := domain.CertListFilter{
		HostingStatus: q.HostingStatus,
		Search:        strings.TrimSpace(q.Search),
	}
	now := time.Now()
	switch q.DaysLeft {
	case "":
	case DaysLeftExpired:
		to := now
		f.NotAfterTo = &to
	case DaysLeftLE7, DaysLeftLE14, DaysLeftLE30:
		days := map[DaysLeftTier]int{
			DaysLeftLE7: 7, DaysLeftLE14: 14, DaysLeftLE30: 30,
		}[q.DaysLeft]
		from, to := now, now.Add(time.Duration(days)*24*time.Hour)
		f.NotAfterFrom, f.NotAfterTo = &from, &to
	case DaysLeftGT30:
		from := now.Add(30 * 24 * time.Hour)
		f.NotAfterFrom = &from
	default:
		return domain.CertListFilter{}, fmt.Errorf("cert: invalid daysLeft filter %q", q.DaysLeft)
	}
	return f, nil
}

// validHostingStatus 枚举校验（空=不筛）。
func validHostingStatus(v domain.HostingStatus) bool {
	return v == "" || v == domain.HostingStatusComplete || v == domain.HostingStatusFingerprintOnly
}

// toListItem 台账文档 → 列表项（daysLeft 向下取整，已过期为负）。
func toListItem(c domain.Certificate, refCount int, now time.Time) CertListItem {
	return CertListItem{
		ID:            c.ID.Hex(),
		Fingerprint:   c.Fingerprint,
		CommonName:    c.CommonName,
		Sans:          nonNilSans(c.Sans),
		Issuer:        c.Issuer,
		NotAfter:      c.NotAfter,
		DaysLeft:      daysLeft(c.NotAfter, now),
		HostingStatus: c.HostingStatus,
		MaterialIssue: c.MaterialIssue,
		ProtectUntil:  c.ProtectUntil,
		RefCount:      refCount,
	}
}

// daysLeft 剩余整天数（向下取整；已过期为负值）。
func daysLeft(notAfter, now time.Time) int {
	return int(math.Floor(notAfter.Sub(now).Hours() / 24))
}

// nonNilSans SAN 空数组语义（JSON 序列化为 [] 而非 null）。
func nonNilSans(sans []string) []string {
	if sans == nil {
		return []string{}
	}
	return sans
}

// referenceCounts 最新成功快照各指纹引用计数（列表 refCount 数据源；
// 无成功快照=空 map，全部 refCount=0）。
func (s *ledgerService) referenceCounts(ctx context.Context) (map[string]int, error) {
	snap, err := s.snapshots.LatestDone(ctx)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return map[string]int{}, nil
	}
	if err != nil {
		return nil, err
	}
	refs, err := s.refs.ListBySnapshotID(ctx, snap.ID.Hex())
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(refs))
	for _, r := range refs {
		counts[r.CertFingerprint]++
	}
	return counts, nil
}

// ---------------------------------------------------------------------
// 详情 / 删除
// ---------------------------------------------------------------------

// GetCert 全要素详情 + 引用三态派生。
func (s *ledgerService) GetCert(ctx context.Context, id string) (CertDetail, error) {
	cert, err := s.certs.GetByID(ctx, id)
	if err != nil {
		return CertDetail{}, err // ErrInvalidID / mongo.ErrNoDocuments
	}
	view, err := s.deriveReferenceStatus(ctx, cert.Fingerprint)
	if err != nil {
		return CertDetail{}, err
	}
	return toDetail(cert, view, time.Now()), nil
}

// DeleteCert 删除拦截（tech-design）：has_refs 与 blind_spot 一律 409 CERT_HAS_REFS
// （blind_spot 附盲区原因）；仅 no_refs_scanned 且不在保护期（protectUntil < now
// 或缺省）放行，保护期内拦截附截止时间。
func (s *ledgerService) DeleteCert(ctx context.Context, id string) error {
	cert, err := s.certs.GetByID(ctx, id)
	if err != nil {
		return err
	}
	view, err := s.deriveReferenceStatus(ctx, cert.Fingerprint)
	if err != nil {
		return err
	}
	switch view.Status {
	case domain.RefStatusHasRefs, domain.RefStatusBlindSpot:
		return &domain.DeleteBlockedError{
			ReferenceStatus: view.Status,
			RefCount:        view.RefCount,
			Reason:          view.Reason,
		}
	}
	// no_refs_scanned：保护期另计（>=now 禁删，schema protectUntil 注释）
	if cert.ProtectUntil != nil && !cert.ProtectUntil.Before(time.Now()) {
		return &domain.DeleteBlockedError{
			ReferenceStatus: view.Status,
			Reason: fmt.Sprintf("within rollback protection period until %s",
				cert.ProtectUntil.UTC().Format(time.RFC3339)),
			ProtectUntil: cert.ProtectUntil,
		}
	}
	return s.certs.DeleteByFingerprint(ctx, cert.Fingerprint)
}

// toDetail 台账文档 → 详情（HasKey=存在密文私钥；永不复制密文本身）。
func toDetail(c domain.Certificate, view refStatusView, now time.Time) CertDetail {
	return CertDetail{
		ID:               c.ID.Hex(),
		Fingerprint:      c.Fingerprint,
		CommonName:       c.CommonName,
		Sans:             nonNilSans(c.Sans),
		Issuer:           c.Issuer,
		SerialNumber:     c.SerialNumber,
		NotBefore:        c.NotBefore,
		NotAfter:         c.NotAfter,
		DaysLeft:         daysLeft(c.NotAfter, now),
		KeyAlgorithm:     c.KeyAlgorithm,
		HostingStatus:    c.HostingStatus,
		MaterialIssue:    c.MaterialIssue,
		HasKey:           c.EncryptedPrivateKey != nil,
		ExpectedDomain:   c.ExpectedDomain,
		ProtectUntil:     c.ProtectUntil,
		ExpiryAlertLevel: c.ExpiryAlertLevel,
		CreatedAt:        c.CreatedAt,
		RefCount:         view.RefCount,
		ReferenceStatus:  view.Status,
	}
}

// refStatusView / deriveRefStatusFor 抽至 reference_query_service.go
// （任务 3.6 共享位置，台账与引用视图复用，避免复制）。

// deriveReferenceStatus 引用三态派生（tech-design"引用状态语义"，"未发现引用"≠"无引用"）：
// 数据拉取编排 + 委托共享派生核心 deriveRefStatusFor。
// "涉及云/产品"取该指纹全部历史引用（跨快照累计视图）的去重 (cloud,product) 对；
// 历史无引用时涉及集为空，空集⊆任意扫描范围，视作已覆盖（成功快照存在即 no_refs_scanned）。
func (s *ledgerService) deriveReferenceStatus(ctx context.Context, fingerprint string) (refStatusView, error) {
	snap, err := s.snapshots.LatestDone(ctx)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return deriveRefStatusFor(fingerprint, nil, nil, nil)
	}
	if err != nil {
		return refStatusView{}, err
	}
	refs, err := s.refs.ListBySnapshotID(ctx, snap.ID.Hex())
	if err != nil {
		return refStatusView{}, err
	}
	return deriveRefStatusFor(fingerprint, &snap, refs, func() ([]domain.CertReference, error) {
		return s.refs.ListByFingerprint(ctx, fingerprint)
	})
}

// ---------------------------------------------------------------------
// 统计
// ---------------------------------------------------------------------

// Stats 双口径覆盖率（Hard Rule：每次查询实时聚合，不落存储快照）。
// 分母 = 最新成功（status=done）快照 CertReference 指纹去重 ∪ 台账全部指纹；
// missingRegistrations=扫描发现未登记数；fingerprintOnlyRate=台账内占比。
func (s *ledgerService) Stats(ctx context.Context) (LedgerStats, error) {
	certs, err := s.certs.List(ctx)
	if err != nil {
		return LedgerStats{}, err
	}

	var st LedgerStats
	ledger := make(map[string]struct{}, len(certs))
	for _, c := range certs {
		ledger[c.Fingerprint] = struct{}{}
		switch c.HostingStatus {
		case domain.HostingStatusComplete:
			st.Complete++
		case domain.HostingStatusFingerprintOnly:
			st.FingerprintOnly++
		}
	}
	st.Total = len(certs)

	// 最新成功快照指纹去重集合（failed/running 快照不作分母来源）
	scanned := map[string]struct{}{}
	snap, err := s.snapshots.LatestDone(ctx)
	switch {
	case errors.Is(err, mongo.ErrNoDocuments):
		// 无成功快照：扫描集合为空，并集自然退化为台账集合（非口径退化）
	case err != nil:
		return LedgerStats{}, err
	default:
		refs, err := s.refs.ListBySnapshotID(ctx, snap.ID.Hex())
		if err != nil {
			return LedgerStats{}, err
		}
		for _, r := range refs {
			scanned[r.CertFingerprint] = struct{}{}
		}
	}

	denominator := make(map[string]struct{}, len(ledger)+len(scanned))
	for fp := range ledger {
		denominator[fp] = struct{}{}
	}
	for fp := range scanned {
		if _, ok := ledger[fp]; !ok {
			denominator[fp] = struct{}{}
			st.MissingRegistrations++ // 扫描发现未登记 = 登记缺口
		}
	}
	manualOnly := 0
	for fp := range ledger {
		if _, ok := scanned[fp]; !ok {
			manualOnly++
		}
	}
	st.Denominator = len(denominator)
	st.DenominatorSources = DenominatorSources{
		ScannedUniqueFingerprints: len(scanned),
		ManualOnlyFingerprints:    manualOnly,
	}
	st.RegistrationRate = ratio(st.Total, st.Denominator)
	st.ReplaceableRate = ratio(st.Complete, st.Denominator)
	st.FingerprintOnlyRate = ratio(st.FingerprintOnly, st.Total)
	return st, nil
}

// ratio x/y；y=0 时取 0，结果四舍五入至万分位（稳定展示）。
func ratio(x, y int) float64 {
	if y == 0 {
		return 0
	}
	return math.Round(float64(x)/float64(y)*10000) / 10000
}

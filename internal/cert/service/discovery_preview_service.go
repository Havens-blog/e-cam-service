package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------------------------------------------------------------------
// 云端发现导入：发现预览与快照状态查询（cert-cloud-discovery-import 任务 3）
// ---------------------------------------------------------------------

// unresolvedFingerprintPrefix 占位指纹公式前缀（与 reference_scan_service
// resolveUncached 同源）。占位指纹是确定性可重算值（proposal Key Scenarios：
// 按 certscan-unresolved:{cloud}|{accountKey}|{certId} 公式由引用三元组重得），
// 预览按同公式识别占位条目打"导入时解析"标记。
const unresolvedFingerprintPrefix = "certscan-unresolved"

// 可解析标记原因码（parseable=false 归入不可选组；deferred_parse 保持可选）。
const (
	// DiscoveryParseDeferred 占位指纹条目：导入时由 GetCert 解析真实指纹
	//（parseable=true，导入期解析成功则正常登记并回填引用）。
	DiscoveryParseDeferred = "deferred_parse"
	// DiscoveryParseUnsupportedCloud 华为云：SCM 无 PEM 通道（SHA-1 口径），
	// 整组不可选（parseable=false）。
	DiscoveryParseUnsupportedCloud = "unsupported_cloud"
	// DiscoveryParseIAMHosted AWS IAM-hosted（非 arn: 前缀证书 ID）：GetCert
	// 不支持该形态，同华为语义降级不可选（parseable=false）。
	DiscoveryParseIAMHosted = "iam_hosted"
)

// placeholderFingerprintFor 按引用三元组重算扫描侧确定性占位指纹
// （cacheKey 口径与 resolveFingerprint 一致：cloud|accountKey|cloudCertId）。
func placeholderFingerprintFor(cloud, accountKey, cloudCertID string) string {
	return sha256Hex(unresolvedFingerprintPrefix + ":" + strings.Join([]string{cloud, accountKey, cloudCertID}, "|"))
}

// DiscoveryPreviewEntry 发现预览唯一证书条目（cloud+accountKey+cloudCertId
// 三元组去重；排除 product=crd 引用与空 cloud 条目）。
type DiscoveryPreviewEntry struct {
	Cloud       string
	AccountKey  string
	CloudCertID string
	Label       string // 可读名（引用 resourceId 采样：cas=证书名称、cdn/waf=域名、alb/nlb=复合 ID）；空=引用未带
	RefCount    int    // 快照内该三元组的引用资源数
	InLedger    bool
	NotAfter    *time.Time // 双通道命中且台账可查时为台账值；未登记为 nil（web 层占位显示）
	Parseable   bool       // false=不可选组（华为云/AWS IAM-hosted）
	ParseReason string     // deferred_parse / unsupported_cloud / iam_hosted；常态空
}

// DiscoveryPreview 发现预览结果（GET /discovery/preview 服务结果）。
type DiscoveryPreview struct {
	SnapshotID        string
	SnapshotStartedAt time.Time // 前端快照超 7 天重扫提示依据
	Items             []DiscoveryPreviewEntry
}

// DiscoverySnapshotStatus 快照状态查询结果（GET /discovery/snapshot-status）。
// 零快照空态：HasSnapshot=false（200 空态响应，区别于 preview 的 NO_SNAPSHOT
// 409 引导语义——前者引导"触发首次扫描"，后者引导"等待/重扫后进入预览"）。
type DiscoverySnapshotStatus struct {
	HasSnapshot     bool
	SnapshotID      string
	Status          domain.ScanStatus // running/done/failed；零快照为空
	StartedAt       time.Time
	FailReason      string
	PartialFailures []domain.ScanChannelFailure
}

// DiscoveryPreviewService 云端发现导入只读查询服务（任务 3）：预览聚合与
// 快照状态查询。Hard Rule：预览为纯 DB 聚合——本服务不持有任何云适配器/
// 账号源依赖（编译期即无云 API 调用面），云 API 后置到任务 4 导入编排。
type DiscoveryPreviewService interface {
	// Preview 基于最近 done 快照聚合唯一证书清单；无 done 快照返回
	// domain.ErrNoSnapshot（409 NO_SNAPSHOT）。
	Preview(ctx context.Context) (DiscoveryPreview, error)
	// SnapshotStatus 返回最近快照（不限状态）的 status/startedAt/
	// partialFailures；零快照返回 HasSnapshot=false 空态（非错误）。
	SnapshotStatus(ctx context.Context) (DiscoverySnapshotStatus, error)
}

type discoveryPreviewService struct {
	snapshots domain.ScanSnapshotRepository
	refs      domain.CertReferenceRepository
	certs     domain.CertificateRepository
	mappings  domain.CloudCertMappingRepository
}

// NewDiscoveryPreviewService 创建发现预览服务。
func NewDiscoveryPreviewService(
	snapshots domain.ScanSnapshotRepository,
	refs domain.CertReferenceRepository,
	certs domain.CertificateRepository,
	mappings domain.CloudCertMappingRepository,
) DiscoveryPreviewService {
	return &discoveryPreviewService{snapshots: snapshots, refs: refs, certs: certs, mappings: mappings}
}

// previewEntryAccum 去重聚合累加器（三元组 → 引用计数与指纹集合）。
type previewEntryAccum struct {
	cloud        string
	accountKey   string
	cloudCertID  string
	refCount     int
	label        string // 首个引用的 resourceId（cas=证书名称、cdn/waf=域名、alb/nlb=复合 ID），预览可读性
	fingerprints map[string]struct{}
}

// Preview 编排：最近 done 快照（无 → ErrNoSnapshot）→ 快照引用按三元组去重
// 聚合（排除 crd/空 cloud）→ 台账指纹与 CloudCertMapping 双通道 inLedger 判定
// → 可解析标记（华为/IAM-hosted/占位指纹）。全程仅仓储读取，无云 API。
func (s *discoveryPreviewService) Preview(ctx context.Context) (DiscoveryPreview, error) {
	snap, err := s.snapshots.LatestDone(ctx)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return DiscoveryPreview{}, domain.ErrNoSnapshot
	}
	if err != nil {
		return DiscoveryPreview{}, err
	}
	refs, err := s.refs.ListBySnapshotID(ctx, snap.ID.Hex())
	if err != nil {
		return DiscoveryPreview{}, err
	}
	ledgerCerts, err := s.certs.List(ctx)
	if err != nil {
		return DiscoveryPreview{}, err
	}
	ledgerByFP := make(map[string]domain.Certificate, len(ledgerCerts))
	for _, c := range ledgerCerts {
		ledgerByFP[c.Fingerprint] = c
	}

	// 三元组去重聚合（排除 product=crd 引用与空 cloud 条目，SC-1 去重公式）
	type entryKey struct{ cloud, accountKey, cloudCertID string }
	order := make([]entryKey, 0, 8)
	byKey := make(map[entryKey]*previewEntryAccum, 8)
	for _, r := range refs {
		if r.Product == domain.ProductCRD || r.Cloud == "" {
			continue
		}
		k := entryKey{cloud: string(r.Cloud), accountKey: r.AccountKey, cloudCertID: r.ReferencedCloudCertID}
		acc, ok := byKey[k]
		if !ok {
			acc = &previewEntryAccum{cloud: k.cloud, accountKey: k.accountKey, cloudCertID: k.cloudCertID}
			byKey[k] = acc
			order = append(order, k)
		}
		acc.refCount++
		if acc.label == "" && r.ResourceID != "" {
			acc.label = r.ResourceID
		}
		if r.CertFingerprint != "" {
			if acc.fingerprints == nil {
				acc.fingerprints = make(map[string]struct{}, 2)
			}
			acc.fingerprints[r.CertFingerprint] = struct{}{}
		}
	}
	sort.Slice(order, func(i, j int) bool { // 字典序稳定（cloud→accountKey→cloudCertId）
		if order[i].cloud != order[j].cloud {
			return order[i].cloud < order[j].cloud
		}
		if order[i].accountKey != order[j].accountKey {
			return order[i].accountKey < order[j].accountKey
		}
		return order[i].cloudCertID < order[j].cloudCertID
	})

	items := make([]DiscoveryPreviewEntry, 0, len(order))
	for _, k := range order {
		acc := byKey[k]
		entry, err := s.buildEntry(ctx, acc, ledgerByFP)
		if err != nil {
			return DiscoveryPreview{}, err
		}
		items = append(items, entry)
	}
	return DiscoveryPreview{
		SnapshotID:        snap.ID.Hex(),
		SnapshotStartedAt: snap.StartedAt,
		Items:             items,
	}, nil
}

// buildEntry 单条目字段派生：inLedger 双通道（台账指纹命中 →
// FindByCloudCertID 映射命中）→ notAfter（台账值）→ 可解析标记。
// 映射仓储读取故障时返回错误（预览为单一只读目的，仓储故障应显式 500
// 而非降级误判可导入——区别于扫描链路的容错跳过口径）。
func (s *discoveryPreviewService) buildEntry(
	ctx context.Context,
	acc *previewEntryAccum,
	ledgerByFP map[string]domain.Certificate,
) (DiscoveryPreviewEntry, error) {
	e := DiscoveryPreviewEntry{
		Cloud:       acc.cloud,
		AccountKey:  acc.accountKey,
		CloudCertID: acc.cloudCertID,
		Label:       acc.label,
		RefCount:    acc.refCount,
	}

	// 通道一：引用指纹在台账（占位指纹为公式哈希，不会命中真实证书指纹）
	for fp := range acc.fingerprints {
		if c, ok := ledgerByFP[fp]; ok {
			e.InLedger = true
			na := c.NotAfter
			e.NotAfter = &na
			break
		}
	}
	// 通道二：本云本账号映射 FindByCloudCertID（占位指纹条目导入后重跑预览
	// 的灰选依据——指纹通道对占位条目恒 miss，映射通道命中即 inLedger）
	if !e.InLedger {
		m, err := s.mappings.FindByCloudCertID(ctx, acc.cloud, acc.accountKey, acc.cloudCertID)
		switch {
		case err == nil:
			e.InLedger = true
			if c, ok := ledgerByFP[m.CertFingerprint]; ok {
				na := c.NotAfter
				e.NotAfter = &na
			}
		case errors.Is(err, mongo.ErrNoDocuments):
			// 无映射=未登记，正常落空
		default:
			return DiscoveryPreviewEntry{}, err
		}
	}

	_, isPlaceholder := acc.fingerprints[placeholderFingerprintFor(acc.cloud, acc.accountKey, acc.cloudCertID)]
	e.Parseable = true
	switch {
	case acc.cloud == string(domain.CloudHuawei):
		e.Parseable = false
		e.ParseReason = DiscoveryParseUnsupportedCloud
	case acc.cloud == string(domain.CloudAWS) && !strings.HasPrefix(acc.cloudCertID, "arn:"):
		e.Parseable = false
		e.ParseReason = DiscoveryParseIAMHosted
	case isPlaceholder:
		e.ParseReason = DiscoveryParseDeferred // 仍可选：导入时 GetCert 解析
	}
	return e, nil
}

// SnapshotStatus 最近快照状态（不限状态：running/done/failed）。零快照空态
// 定义为 HasSnapshot=false 的 200 响应（NO_SNAPSHOT 仅属 preview——空态与
// 引导语义分离，前端按 hasSnapshot 分流"首次扫描"与"轮询等待"）。
func (s *discoveryPreviewService) SnapshotStatus(ctx context.Context) (DiscoverySnapshotStatus, error) {
	snap, err := s.snapshots.Latest(ctx)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return DiscoverySnapshotStatus{PartialFailures: []domain.ScanChannelFailure{}}, nil
	}
	if err != nil {
		return DiscoverySnapshotStatus{}, err
	}
	partials := append([]domain.ScanChannelFailure(nil), snap.PartialFailures...)
	if partials == nil {
		partials = []domain.ScanChannelFailure{}
	}
	return DiscoverySnapshotStatus{
		HasSnapshot:     true,
		SnapshotID:      snap.ID.Hex(),
		Status:          snap.Status,
		StartedAt:       snap.StartedAt,
		FailReason:      snap.FailReason,
		PartialFailures: partials,
	}, nil
}

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"

	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/k8s"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aliyun"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aws"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/azure"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/huawei"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/tencent"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ---------------------------------------------------------------------
// 注入端口（Implementation Notes：cloudx 适配以接口注入，便于 mock）
// ---------------------------------------------------------------------

// DiscoveredRef 云侧证书引用统一形态（各云适配 CloudCertRef 字段同构，经
// shim 归一；对齐 schema.sql cert_references 落库字段）。
type DiscoveredRef struct {
	Cloud                 string
	Product               string
	ResourceID            string
	ReferencedCloudCertID string
	AccountKey            string
}

// CloudCertStatus GetCert 返回的云侧证书在库状态（指纹解析 fallback 数据）。
// Fingerprint 为 SHA256 hex 对齐口径时空串（华为云/腾讯云 SHA-1 口径"无法复核"，
// 见各适配注释），此时按"无法解析"处理。
type CloudCertStatus struct {
	Exists      bool
	NotAfter    time.Time
	Fingerprint string
}

// CloudScanAdapter 单云引用发现端口（3.1/3.2 五方法适配与 3.3 discovery-only
// 适配经 shim 注入）。
//
// Hard Rule（任务 3.5）：扫描为只读操作——本端口仅暴露只读面
// （ListReferences/GetCert），不提供 UploadCert/BindResource/CleanupOrphan
// 任何写通路；discovery-only 约束同样适用于 aliyun/tencent 的发现面。
type CloudScanAdapter interface {
	// Cloud 适配归属云（aliyun|tencent|huawei|aws|azure）。
	Cloud() domain.Cloud
	// Products 该云支持的证书产品枚举（扫描范围组成）。
	Products() []domain.Product
	// ListReferences 只读发现产品下全部证书引用。
	ListReferences(ctx context.Context, creds *sharedomain.CloudAccount, product domain.Product) ([]DiscoveredRef, error)
	// GetCert 查询云侧证书在库状态（指纹解析 fallback，只读）。
	GetCert(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (CloudCertStatus, error)
}

// K8sObject 单个 CRD 实例（unstructured 内容 + 定位三要素）。
type K8sObject struct {
	Namespace string
	Name      string
	Content   map[string]interface{}
}

// K8sScanGateway K8s 引用发现端口（3.4 dynamic client 工厂之上的薄抽象，
// fake 注入支撑集成测试）。ListObjects 列出集群内指定 apiGroup+kind 的全部
// 实例（全命名空间）。
type K8sScanGateway interface {
	ListObjects(ctx context.Context, cluster, apiGroup, kind string) ([]K8sObject, error)
}

// ScanAccountSource 扫描目标云账号来源（active 账号；凭证由仓储读取路径解密，
// 仅内存使用，禁入日志/错误信息）。
type ScanAccountSource interface {
	ActiveByCloud(ctx context.Context, cloud domain.Cloud) ([]*sharedomain.CloudAccount, error)
}

// ScanAlertNotifier 扫描告警端口（scan-timeout 恢复的告警事件；nil=no-op）。
// 7.1 调度装配时接入 internal/alert 通道；通知失败不阻塞恢复流程。
type ScanAlertNotifier interface {
	NotifyScanTimedOut(ctx context.Context, snapshotID string, startedAt, recoveredAt time.Time) error
}

// ---------------------------------------------------------------------
// 服务
// ---------------------------------------------------------------------

// ScanResult 单次扫描结果（web 层 POST /:id/scan 响应数据源）。
// 异步触发时 Status=running 且仅 SnapshotID/StartedAt 有意义（后台 runScan 收敛终态）。
type ScanResult struct {
	SnapshotID        string
	Status            domain.ScanStatus
	FailReason        string
	ReferencesWritten int
	ChannelsAttempted int
	ChannelsFailed    int
	PartialFailures   []domain.ScanChannelFailure
	CoverageMeta      []domain.CoverageMeta
	StartedAt         time.Time // running 快照启动时点（异步触发回传，供前端轮询基线）
}

// ReferenceScanService 引用发现编排（任务 3.5）：单次扫描创建 ScanSnapshot
// （running）→ 五云适配 + K8s 登记遍历发现 CertReference → coverageMeta 收敛
// → 终态（done/failed）。防重（running → 409 SCAN_IN_PROGRESS）与 scan-timeout
// 恢复（running 超时转 failed 释放防重锁）。调度注册在 7.1。
type ReferenceScanService interface {
	// StartScan 触发一次全量引用扫描（同步执行至终态；已有 running 快照返回
	// domain.ErrScanInProgress）。调度器调用。
	StartScan(ctx context.Context) (ScanResult, error)
	// StartScanAsync 异步触发：同步建 running 快照后立即返回 running 态结果，
	// 发现+写库+终态在后台 goroutine 跑。HTTP 立即扫描调用，避免长扫描超时。
	StartScanAsync(ctx context.Context) (ScanResult, error)
	// RecoverTimedOutScans scan-timeout 恢复：running 且 now > startedAt +
	// thresholds.scanTimeoutHours → failed（SCAN_TIMED_OUT）+ 告警事件 +
	// 释放防重锁（可重新触发）。返回恢复条数。
	RecoverTimedOutScans(ctx context.Context) (int, error)
}

type referenceScanService struct {
	snapshots domain.ScanSnapshotRepository
	refs      domain.CertReferenceRepository
	mappings  domain.CloudCertMappingRepository
	crdRegs   domain.CrdRegistrationRepository
	alertCfg  domain.AlertConfigRepository
	coverage  *coverageCalculator

	adapters   []CloudScanAdapter // 逐云顺序扫描（Hard Rule：并发受控）
	accounts   ScanAccountSource
	k8sGateway K8sScanGateway
	notifier   ScanAlertNotifier // 可为 nil
}

// NewReferenceScanService 创建引用扫描服务。
func NewReferenceScanService(
	snapshots domain.ScanSnapshotRepository,
	refs domain.CertReferenceRepository,
	mappings domain.CloudCertMappingRepository,
	crdRegs domain.CrdRegistrationRepository,
	alertCfg domain.AlertConfigRepository,
	assets AssetCountSource,
	adapters []CloudScanAdapter,
	accounts ScanAccountSource,
	k8sGateway K8sScanGateway,
	notifier ScanAlertNotifier,
) ReferenceScanService {
	return &referenceScanService{
		snapshots:  snapshots,
		refs:       refs,
		mappings:   mappings,
		crdRegs:    crdRegs,
		alertCfg:   alertCfg,
		coverage:   &coverageCalculator{assets: assets},
		adapters:   adapters,
		accounts:   accounts,
		k8sGateway: k8sGateway,
		notifier:   notifier,
	}
}

// ---------------------------------------------------------------------
// StartScan
// ---------------------------------------------------------------------

// StartScan 全量引用扫描编排（同步至终态）：
//  1. 防重：已有 running 快照 → domain.ErrScanInProgress（409）；
//  2. 账号/范围收集：active 账号的云×产品 + enabled CRD 登记（无账号云不入
//     范围——引用三态按 coverageMeta 范围判定，空扫不可声明"已扫描"）；
//  3. 分母固化（扫描启动时点，asset 盘点独立聚合）→ 创建 running 快照；
//  4. 逐云顺序发现（部分失败不阻塞其他云，记入 partialFailures）；
//  5. 写引用（certFingerprint 解析 + snapshotId/scannedAt 写通）；
//  6. 收敛：coverageMeta（covered=去重资源数）+ 终态（done / failed）。
// StartScan 全量引用扫描编排（同步至终态；调度器调用）：beginScan + runScan 串行。
// HTTP 立即扫描走 StartScanAsync（beginScan 同步建 running 快照 + 后台 runScan），
// 避免真实账号发现+GetCert 指纹解析耗时超过 HTTP/axios 30s 超时。
func (s *referenceScanService) StartScan(ctx context.Context) (ScanResult, error) {
	sc, early, err := s.beginScan(ctx)
	if err != nil {
		return ScanResult{}, err
	}
	if early != nil {
		return *early, nil // 空范围：快照已 finish failed
	}
	return s.runScan(ctx, sc)
}

// StartScanAsync 异步触发：beginScan 同步建 running 快照后立即返回 running 态
// ScanResult，发现+写库+终态在后台 goroutine 跑（context.Background，不受请求
// ctx 释放影响）。防重语义同 StartScan（已有 running 快照 → ErrScanInProgress）。
// 卡死/panic 由 RecoverTimedOutScans（scanTimeoutHours）兜底释放防重锁。
func (s *referenceScanService) StartScanAsync(ctx context.Context) (ScanResult, error) {
	sc, early, err := s.beginScan(ctx)
	if err != nil {
		return ScanResult{}, err
	}
	if early != nil {
		return *early, nil // 空范围：同步 finish failed，无后台任务
	}
	go s.runScan(context.Background(), sc)
	return ScanResult{SnapshotID: sc.snapID, Status: domain.ScanStatusRunning, StartedAt: sc.startedAt}, nil
}

// scanContext 扫描运行期上下文：beginScan 收集、runScan 消费。
type scanContext struct {
	snapID          string
	startedAt       time.Time
	scope           []CloudProductKey
	accountsByCloud map[domain.Cloud][]*sharedomain.CloudAccount
	k8sRegs         []domain.CrdRegistration
	totals          map[CloudProductKey]int
	history         []domain.CoverageMeta
}

// beginScan 扫描前置：防重 → 账号/范围收集 → 分母固化 → 建 running 快照。
// 空范围时同步 finish failed 并经 early 返回终态 ScanResult（无后台任务）。
func (s *referenceScanService) beginScan(ctx context.Context) (sc scanContext, early *ScanResult, err error) {
	// 1. 防重
	if _, err := s.snapshots.LatestRunning(ctx); err == nil {
		return scanContext{}, nil, domain.ErrScanInProgress
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return scanContext{}, nil, err
	}

	// 2. 账号/范围收集
	accountsByCloud := make(map[domain.Cloud][]*sharedomain.CloudAccount)
	scope := make([]CloudProductKey, 0)
	for _, adapter := range s.adapters {
		accounts, err := s.accounts.ActiveByCloud(ctx, adapter.Cloud())
		if err != nil {
			return scanContext{}, nil, fmt.Errorf("cert: scan load accounts for %s: %w", adapter.Cloud(), err)
		}
		if len(accounts) == 0 {
			continue // 该云无 active 账号：不入扫描范围（该云保持盲区声明）
		}
		accountsByCloud[adapter.Cloud()] = accounts
		for _, p := range adapter.Products() {
			scope = append(scope, CloudProductKey{Cloud: adapter.Cloud(), Product: p})
		}
	}
	k8sRegs, err := s.crdRegs.ListEnabled(ctx)
	if err != nil {
		return scanContext{}, nil, err
	}
	if len(k8sRegs) > 0 {
		scope = append(scope, k8sCoverageKey)
	}

	// 3. 分母固化（启动时点）+ 创建 running 快照
	prevDone, err := s.snapshots.LatestDone(ctx)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return scanContext{}, nil, err
	}
	var history []domain.CoverageMeta
	if err == nil {
		history = prevDone.CoverageMeta
	}
	totals := s.coverage.snapshotTotals(ctx, scope, history)
	snap := &domain.ScanSnapshot{Status: domain.ScanStatusRunning, CoverageMeta: metaFromTotals(totals)}
	snapID, err := s.snapshots.Create(ctx, snap)
	if err != nil {
		return scanContext{}, nil, err
	}

	// 空范围（无账号且无登记）：显式失败——空范围快照会使引用三态的"已扫描"
	// 声明失真（全部证书被判 no_refs_scanned）
	if len(scope) == 0 {
		failReason := domain.FailReasonScanNoChannels
		if ferr := s.snapshots.FinishScan(ctx, snapID, domain.ScanStatusFailed, failReason, nil, nil); ferr != nil {
			return scanContext{}, nil, ferr
		}
		return scanContext{}, &ScanResult{SnapshotID: snapID, Status: domain.ScanStatusFailed, FailReason: failReason}, nil
	}

	return scanContext{
		snapID:          snapID,
		startedAt:       snap.StartedAt,
		scope:           scope,
		accountsByCloud: accountsByCloud,
		k8sRegs:         k8sRegs,
		totals:          totals,
		history:         history,
	}, nil, nil
}

// runScan 扫描主干：逐云顺序发现 + K8s 发现 + 写引用 + coverageMeta 收敛 + 终态。
// 同步/异步共用；ctx 在异步路径为 context.Background（不受请求生命周期影响）。
func (s *referenceScanService) runScan(ctx context.Context, sc scanContext) (ScanResult, error) {
	snapID := sc.snapID
	totals := sc.totals
	k8sRegs := sc.k8sRegs
	accountsByCloud := sc.accountsByCloud
	var (
		discovered []domain.CertReference
		partials   []domain.ScanChannelFailure
		attempted  int
		failed     int
		fpCache    = make(map[string]string)
	)

	// 4. 发现（逐云顺序）
	for _, adapter := range s.adapters {
		accounts := accountsByCloud[adapter.Cloud()]
		if len(accounts) == 0 {
			continue
		}
		for _, product := range adapter.Products() {
			for _, account := range accounts {
				attempted++
				refs, err := adapter.ListReferences(ctx, account, product)
				if err != nil {
					failed++
					partials = append(partials, domain.ScanChannelFailure{
						Cloud:   string(adapter.Cloud()),
						Product: string(product),
						Account: account.Name,
						Reason:  scanFailureReason(err),
					})
					continue // 部分失败不阻塞其他云/产品
				}
				for _, r := range refs {
					if r.ResourceID == "" || r.ReferencedCloudCertID == "" {
						continue // 无法定位资源/无证书关联的发现项不构成引用
					}
					fp := s.resolveFingerprint(ctx, adapter, account, r, fpCache)
					discovered = append(discovered, domain.CertReference{
						CertFingerprint:       fp,
						Cloud:                 adapter.Cloud(),
						Product:               product,
						ResourceID:            r.ResourceID,
						ReferencedCloudCertID: r.ReferencedCloudCertID,
						AccountKey:            r.AccountKey,
						SnapshotID:            snapID,
					})
				}
			}
		}
	}

	// K8s 发现：固定枚举（EnsureBuiltinRegistrations 播种）+ enabled 自定义登记
	if len(k8sRegs) > 0 {
		byCluster := make(map[string][]domain.CrdRegistration)
		for _, reg := range k8sRegs {
			byCluster[reg.ClusterID] = append(byCluster[reg.ClusterID], reg)
		}
		for cluster, clusterRegs := range byCluster {
			for _, reg := range clusterRegs {
				attempted++
				objRefs, err := s.discoverK8sRegistration(ctx, cluster, reg, snapID, fpCache)
				if err != nil {
					failed++
					partials = append(partials, domain.ScanChannelFailure{
						Product: string(domain.ProductCRD),
						Account: reg.ClusterID,
						Reason:  scanFailureReason(err),
					})
					continue
				}
				discovered = append(discovered, objRefs...)
			}
		}
	}

	// 5. 写引用（snapshotId/scannedAt 写通；DEFAULT scannedAt=now 由仓储填充）
	written, err := s.refs.CreateMulti(ctx, discovered)
	if err != nil {
		failErr := s.snapshots.FinishScan(ctx, snapID, domain.ScanStatusFailed, domain.FailReasonScanWriteFailed, metaFromTotals(totals), partials)
		if failErr != nil {
			return ScanResult{}, failErr
		}
		return ScanResult{SnapshotID: snapID, Status: domain.ScanStatusFailed, FailReason: domain.FailReasonScanWriteFailed}, err
	}

	// 6. 收敛：covered=本轮去重资源数 + 终态
	finalMeta := finalizeCoverage(totals, coveredResources(discovered))
	status := domain.ScanStatusDone
	failReason := ""
	if failed == attempted { // 全部通道失败 → 整体失败
		status = domain.ScanStatusFailed
		failReason = domain.FailReasonScanDiscoveryFailed
	}
	if err := s.snapshots.FinishScan(ctx, snapID, status, failReason, finalMeta, partials); err != nil {
		return ScanResult{}, err
	}
	return ScanResult{
		SnapshotID:        snapID,
		Status:            status,
		FailReason:        failReason,
		ReferencesWritten: written,
		ChannelsAttempted: attempted,
		ChannelsFailed:    failed,
		PartialFailures:   partials,
		CoverageMeta:      finalMeta,
	}, nil
}

// discoverK8sRegistration 单登记项发现：列出集群内该 apiGroup+kind 全部实例，
// 按 certFieldPath 读取证书引用字段（每值一引用，含 clusterId/namespace/kind）。
func (s *referenceScanService) discoverK8sRegistration(
	ctx context.Context,
	cluster string,
	reg domain.CrdRegistration,
	snapID string,
	fpCache map[string]string,
) ([]domain.CertReference, error) {
	if err := k8s.ValidateCertFieldPath(reg.CertFieldPath); err != nil {
		return nil, err // 登记期已校验；防御性兜底（非法路径报错并记入通道失败）
	}
	objects, err := s.k8sGateway.ListObjects(ctx, cluster, reg.APIGroup, reg.Kind)
	if err != nil {
		return nil, err // ErrK8sUnreachable 等透传，记入通道失败
	}
	var refs []domain.CertReference
	for _, obj := range objects {
		if obj.Name == "" {
			continue
		}
		for _, certID := range extractCertFieldValues(obj.Content, reg.CertFieldPath) {
			refs = append(refs, domain.CertReference{
				CertFingerprint:       s.resolveK8sFingerprint(ctx, cluster, certID, fpCache),
				Product:               domain.ProductCRD,
				ClusterID:             cluster,
				Namespace:             obj.Namespace,
				Kind:                  reg.Kind,
				ResourceID:            obj.Name,
				ReferencedCloudCertID: certID,
				SnapshotID:            snapID,
			})
		}
	}
	return refs, nil
}

// ---------------------------------------------------------------------
// 指纹解析（Implementation Notes：映射反查优先 → GetCert 云侧要素 → 占位指纹）
// ---------------------------------------------------------------------

// sha256Hex 指纹十六进制计算。
func sha256Hex(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// fingerprintPatternRe 台账指纹对齐口径 ^[0-9a-f]{64}$（GetCert fallback 仅接受
// 对齐口径；华为云/腾讯云 SHA-1 口径视为无法解析）。
var fingerprintPatternRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// resolveFingerprint 云引用指纹解析：
//  1. CloudCertMapping 反查（certFingerprint↔cloudCertId）；
//  2. adapter.GetCert 云侧证书要素（仅接受 SHA256 对齐指纹）；
//  3. 无法解析 → 确定性占位指纹（保留引用并以云证书 ID 关联，AC）。
//
// fpCache 逐扫描去重（同云证书多处引用只查一次，降低 GetCert 调用）。
func (s *referenceScanService) resolveFingerprint(
	ctx context.Context,
	adapter CloudScanAdapter,
	account *sharedomain.CloudAccount,
	r DiscoveredRef,
	fpCache map[string]string,
) string {
	cacheKey := strings.Join([]string{string(adapter.Cloud()), r.AccountKey, r.ReferencedCloudCertID}, "|")
	if fp, ok := fpCache[cacheKey]; ok {
		return fp
	}
	fp := s.resolveUncached(ctx, cacheKey, string(adapter.Cloud()), r.AccountKey, r.ReferencedCloudCertID,
		func() (CloudCertStatus, error) {
			return adapter.GetCert(ctx, account, r.ReferencedCloudCertID)
		})
	fpCache[cacheKey] = fp
	return fp
}

// resolveK8sFingerprint K8s 引用指纹解析：仅映射反查（cloud/accountKey 未知，
// 通配反查）；GetCert fallback 不适用（无云凭证上下文）→ 占位指纹。
func (s *referenceScanService) resolveK8sFingerprint(
	ctx context.Context, cluster, certID string, fpCache map[string]string,
) string {
	cacheKey := strings.Join([]string{"k8s", cluster, certID}, "|")
	if fp, ok := fpCache[cacheKey]; ok {
		return fp
	}
	fp := s.resolveUncached(ctx, cacheKey, "", "", certID, nil)
	fpCache[cacheKey] = fp
	return fp
}

// resolveUncached 解析主干：映射反查 → GetCert fallback → 占位指纹。
// 占位指纹 = sha256("certscan-unresolved:" + cacheKey)，满足 ^[0-9a-f]{64}$
// 校验器约束且永不与真实证书指纹（证书 DER SHA256）冲突；人工登记/两段式上传
// 建立映射后，下轮扫描恢复精确关联。
func (s *referenceScanService) resolveUncached(
	ctx context.Context,
	cacheKey, cloud, accountKey, cloudCertID string,
	getCert func() (CloudCertStatus, error),
) string {
	if m, err := s.mappings.FindByCloudCertID(ctx, cloud, accountKey, cloudCertID); err == nil {
		return m.CertFingerprint
	}
	// 无命中（ErrNoDocuments）或映射仓储异常（不中断扫描）均走 fallback
	if getCert != nil {
		if info, err := getCert(); err == nil && info.Exists && fingerprintPatternRe.MatchString(info.Fingerprint) {
			return info.Fingerprint // SHA256 对齐口径（如阿里云 CAS PEM 解析）
		}
	}
	return sha256Hex(unresolvedFingerprintPrefix + ":" + cacheKey)
}

// ---------------------------------------------------------------------
// certFieldPath 值抽取
// ---------------------------------------------------------------------

// extractCertFieldValues 按 certFieldPath 从 CRD 实例内容读取证书引用字段值
// （语法见 k8s.ValidateCertFieldPath："." 分段，段名可选 "[]" 数组下钻）。
// 每命中一个非空字符串值产出一个引用（多证书监听/SNI 多值展开）；
// 数字值转整数字符串（部分 CRD 证书 ID 为数值字面量）。
func extractCertFieldValues(obj map[string]interface{}, path string) []string {
	if obj == nil || path == "" {
		return nil
	}
	current := []interface{}{obj}
	for _, rawSeg := range strings.Split(path, ".") {
		seg, isArray := strings.CutSuffix(rawSeg, "[]")
		var next []interface{}
		for _, node := range current {
			m, ok := node.(map[string]interface{})
			if !ok {
				continue
			}
			v, ok := m[seg]
			if !ok {
				continue
			}
			if isArray {
				if arr, ok := v.([]interface{}); ok {
					next = append(next, arr...)
				} else {
					next = append(next, v) // 单值容错（部分 CRD 单元素免数组）
				}
			} else {
				next = append(next, v)
			}
		}
		current = next
		if len(current) == 0 {
			return nil
		}
	}
	var out []string
	for _, v := range current {
		out = append(out, certRefValueToStrings(v)...)
	}
	return out
}

// certRefValueToStrings 末段值归一化为字符串列表（string/数字/字符串数组）。
func certRefValueToStrings(v interface{}) []string {
	switch t := v.(type) {
	case string:
		if t != "" {
			return []string{t}
		}
	case []interface{}:
		var out []string
		for _, item := range t {
			out = append(out, certRefValueToStrings(item)...)
		}
		return out
	case float64:
		// 数值证书 ID（部分 CRD 字面量为数字）：最小精度表示（8089870.0 → "8089870"）
		return []string{strconv.FormatFloat(t, 'f', -1, 64)}
	case int:
		return []string{strconv.Itoa(t)}
	case int64:
		return []string{strconv.FormatInt(t, 10)}
	}
	return nil
}

// ---------------------------------------------------------------------
// covered 统计（去重资源数）
// ---------------------------------------------------------------------

// coveredResources 本轮发现引用的去重资源数（AC：covered=本轮 CertReference
// 去重资源数）：云引用按 (cloud,product,accountKey,resourceId)、K8s 引用按
// (clusterId,namespace,kind,resourceId) 去重——同资源多证书（主+SNI）计 1。
func coveredResources(refs []domain.CertReference) map[CloudProductKey]int {
	seen := make(map[string]struct{}, len(refs))
	covered := make(map[CloudProductKey]int)
	for _, r := range refs {
		key := resourceDedupKey(r)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		covered[CloudProductKey{Cloud: r.Cloud, Product: r.Product}]++
	}
	return covered
}

// resourceDedupKey 资源去重键。
func resourceDedupKey(r domain.CertReference) string {
	if r.Product == domain.ProductCRD {
		return strings.Join([]string{r.ClusterID, r.Namespace, r.Kind, r.ResourceID}, "|")
	}
	return strings.Join([]string{string(r.Cloud), string(r.Product), r.AccountKey, r.ResourceID}, "|")
}

// metaFromTotals 启动时点 coverageMeta（covered=0 占位，分母固化）。
func metaFromTotals(totals map[CloudProductKey]int) []domain.CoverageMeta {
	keys := make([]CloudProductKey, 0, len(totals))
	for k := range totals {
		keys = append(keys, k)
	}
	sortCoverageKeys(keys)
	out := make([]domain.CoverageMeta, 0, len(keys))
	for _, k := range keys {
		total := totals[k]
		if k == k8sCoverageKey {
			total = -1
		}
		out = append(out, domain.CoverageMeta{
			Cloud:   string(k.Cloud),
			Product: string(k.Product),
			Covered: 0,
			Total:   total,
		})
	}
	return out
}

// scanFailureReason 通道失败原因（静态描述+安全参数；不含凭证/私钥片段）。
func scanFailureReason(err error) string {
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

// ---------------------------------------------------------------------
// RecoverTimedOutScans（scan-timeout 恢复，7.1 以 15 分钟周期调度）
// ---------------------------------------------------------------------

// RecoverTimedOutScans running 且 now > startedAt + thresholds.scanTimeoutHours
// → failed（failReason=SCAN_TIMED_OUT）+ 告警事件 + 释放防重锁
// （running 态消除即可重新触发，消除进程崩溃后 running 卡死的静默死锁）。
func (s *referenceScanService) RecoverTimedOutScans(ctx context.Context) (int, error) {
	timeoutHours := domain.DefaultThresholds().ScanTimeoutHours
	if s.alertCfg != nil {
		if cfg, err := s.alertCfg.Get(ctx); err == nil && cfg.Thresholds.ScanTimeoutHours > 0 {
			timeoutHours = cfg.Thresholds.ScanTimeoutHours
		}
		// 阈值读取失败回退默认（恢复流程不中断）
	}
	cutoff := time.Now().Add(-time.Duration(timeoutHours) * time.Hour)
	snaps, err := s.snapshots.ListRunningBefore(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, snap := range snaps {
		if err := s.snapshots.MarkFinished(ctx, snap.ID.Hex(),
			domain.ScanStatusFailed, domain.FailReasonScanTimedOut); err != nil {
			return recovered, err
		}
		recovered++
		if s.notifier != nil {
			// 告警失败不阻塞恢复（快照已转 failed，下轮不再命中；告警可经
			// 告警系统侧重试）
			_ = s.notifier.NotifyScanTimedOut(ctx, snap.ID.Hex(), snap.StartedAt, time.Now())
		}
	}
	return recovered, nil
}

// ---------------------------------------------------------------------
// 生产适配 shim（各云 3.1/3.2/3.3 适配 → CloudScanAdapter 只读端口）
// ---------------------------------------------------------------------

// cloudScanAdapter 通用 shim：cloud/products 元数据 + 只读方法闭包。
type cloudScanAdapter struct {
	cloud    domain.Cloud
	products []domain.Product
	listRefs func(ctx context.Context, creds *sharedomain.CloudAccount, product domain.Product) ([]DiscoveredRef, error)
	getCert  func(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (CloudCertStatus, error)
}

func (a cloudScanAdapter) Cloud() domain.Cloud        { return a.cloud }
func (a cloudScanAdapter) Products() []domain.Product { return a.products }
func (a cloudScanAdapter) ListReferences(ctx context.Context, creds *sharedomain.CloudAccount, product domain.Product) ([]DiscoveredRef, error) {
	return a.listRefs(ctx, creds, product)
}
func (a cloudScanAdapter) GetCert(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (CloudCertStatus, error) {
	return a.getCert(ctx, creds, cloudCertID)
}

// NewAliyunScanAdapter 阿里云扫描适配（3.1 五方法适配只读面）。
func NewAliyunScanAdapter(a *aliyun.CertAdapter) CloudScanAdapter {
	return cloudScanAdapter{
		cloud:    domain.CloudAliyun,
		products: []domain.Product{domain.ProductCDN, domain.ProductDCDN, domain.ProductWAF, domain.ProductALB, domain.ProductNLB},
		listRefs: func(ctx context.Context, creds *sharedomain.CloudAccount, product domain.Product) ([]DiscoveredRef, error) {
			refs, err := a.ListReferences(ctx, creds, string(product))
			return toDiscoveredRefs(len(refs), func(i int) (string, string, string, string, string) {
				r := refs[i]
				return r.Cloud, r.Product, r.ResourceID, r.ReferencedCloudCertID, r.AccountKey
			}), err
		},
		getCert: func(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (CloudCertStatus, error) {
			info, err := a.GetCert(ctx, creds, cloudCertID)
			return CloudCertStatus{Exists: info.Exists, NotAfter: info.NotAfter, Fingerprint: info.Fingerprint}, err
		},
	}
}

// NewTencentScanAdapter 腾讯云扫描适配（3.2 五方法适配只读面）。
func NewTencentScanAdapter(a *tencent.CertAdapter) CloudScanAdapter {
	return cloudScanAdapter{
		cloud:    domain.CloudTencent,
		products: []domain.Product{domain.ProductCDN, domain.ProductWAF, domain.ProductCLB},
		listRefs: func(ctx context.Context, creds *sharedomain.CloudAccount, product domain.Product) ([]DiscoveredRef, error) {
			refs, err := a.ListReferences(ctx, creds, string(product))
			return toDiscoveredRefs(len(refs), func(i int) (string, string, string, string, string) {
				r := refs[i]
				return r.Cloud, r.Product, r.ResourceID, r.ReferencedCloudCertID, r.AccountKey
			}), err
		},
		getCert: func(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (CloudCertStatus, error) {
			info, err := a.GetCert(ctx, creds, cloudCertID)
			return CloudCertStatus{Exists: info.Exists, NotAfter: info.NotAfter, Fingerprint: info.Fingerprint}, err
		},
	}
}

// NewHuaweiScanAdapter 华为云扫描适配（3.3 discovery-only 只读面）。
func NewHuaweiScanAdapter(a *huawei.CertDiscoveryAdapter) CloudScanAdapter {
	return cloudScanAdapter{
		cloud:    domain.CloudHuawei,
		products: []domain.Product{domain.ProductCDN, domain.ProductWAF, domain.ProductALB, domain.ProductNLB},
		listRefs: func(ctx context.Context, creds *sharedomain.CloudAccount, product domain.Product) ([]DiscoveredRef, error) {
			refs, err := a.ListReferences(ctx, creds, string(product))
			return toDiscoveredRefs(len(refs), func(i int) (string, string, string, string, string) {
				r := refs[i]
				return r.Cloud, r.Product, r.ResourceID, r.ReferencedCloudCertID, r.AccountKey
			}), err
		},
		getCert: func(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (CloudCertStatus, error) {
			info, err := a.GetCert(ctx, creds, cloudCertID)
			return CloudCertStatus{Exists: info.Exists, NotAfter: info.NotAfter, Fingerprint: info.Fingerprint}, err
		},
	}
}

// NewAwsScanAdapter AWS 扫描适配（3.3 discovery-only 只读面）。
func NewAwsScanAdapter(a *aws.CertDiscoveryAdapter) CloudScanAdapter {
	return cloudScanAdapter{
		cloud:    domain.CloudAWS,
		products: []domain.Product{domain.ProductCDN, domain.ProductALB, domain.ProductNLB},
		listRefs: func(ctx context.Context, creds *sharedomain.CloudAccount, product domain.Product) ([]DiscoveredRef, error) {
			refs, err := a.ListReferences(ctx, creds, string(product))
			return toDiscoveredRefs(len(refs), func(i int) (string, string, string, string, string) {
				r := refs[i]
				return r.Cloud, r.Product, r.ResourceID, r.ReferencedCloudCertID, r.AccountKey
			}), err
		},
		getCert: func(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (CloudCertStatus, error) {
			info, err := a.GetCert(ctx, creds, cloudCertID)
			return CloudCertStatus{Exists: info.Exists, NotAfter: info.NotAfter, Fingerprint: info.Fingerprint}, err
		},
	}
}

// NewAzureScanAdapter Azure 扫描适配（3.3 discovery-only 只读面）。
func NewAzureScanAdapter(a *azure.CertDiscoveryAdapter) CloudScanAdapter {
	return cloudScanAdapter{
		cloud:    domain.CloudAzure,
		products: []domain.Product{domain.ProductCDN, domain.ProductALB},
		listRefs: func(ctx context.Context, creds *sharedomain.CloudAccount, product domain.Product) ([]DiscoveredRef, error) {
			refs, err := a.ListReferences(ctx, creds, string(product))
			return toDiscoveredRefs(len(refs), func(i int) (string, string, string, string, string) {
				r := refs[i]
				return r.Cloud, r.Product, r.ResourceID, r.ReferencedCloudCertID, r.AccountKey
			}), err
		},
		getCert: func(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (CloudCertStatus, error) {
			info, err := a.GetCert(ctx, creds, cloudCertID)
			return CloudCertStatus{Exists: info.Exists, NotAfter: info.NotAfter, Fingerprint: info.Fingerprint}, err
		},
	}
}

// toDiscoveredRefs 五元组取值闭包 → 统一引用形态转换。
func toDiscoveredRefs(n int, get func(i int) (string, string, string, string, string)) []DiscoveredRef {
	out := make([]DiscoveredRef, 0, n)
	for i := 0; i < n; i++ {
		cloud, product, resourceID, certID, accountKey := get(i)
		out = append(out, DiscoveredRef{
			Cloud: cloud, Product: product, ResourceID: resourceID,
			ReferencedCloudCertID: certID, AccountKey: accountKey,
		})
	}
	return out
}

// ---------------------------------------------------------------------
// K8s / 账号源生产实现
// ---------------------------------------------------------------------

// k8sScanGateway K8s 引用发现生产实现（3.4 dynamic client 工厂）：
// 内置固定枚举精确 GVR；自定义登记按 version 候选探测（plural 小写+ s 启发式，
// 首期 PoC 5.12 校准），全部候选未命中按通道失败处理。
type k8sScanGateway struct {
	factory *k8s.Factory
}

// NewK8sScanGateway 创建 K8s 扫描网关。
func NewK8sScanGateway(factory *k8s.Factory) K8sScanGateway {
	return &k8sScanGateway{factory: factory}
}

// ListObjects 列出集群内指定 apiGroup+kind 全部实例（全命名空间，只读）。
func (g *k8sScanGateway) ListObjects(ctx context.Context, cluster, apiGroup, kind string) ([]K8sObject, error) {
	client, err := g.factory.Client(ctx, cluster)
	if err != nil {
		return nil, err // mongo.ErrNoDocuments（未登记集群）/ ErrK8sUnreachable 透传
	}
	return listCRDObjects(ctx, cluster, apiGroup, kind, client)
}

// crdLister 单集群 CRD 列表能力（k8s.Client 天然满足；测试注入 fake）。
type crdLister interface {
	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error)
}

// listCRDObjects 候选 GVR 探测主干：内置枚举精确命中即返回；自定义登记逐候选
// version 尝试（未命中继续、其余错误透传）；全部候选未命中按通道失败语义报错。
func listCRDObjects(ctx context.Context, cluster, apiGroup, kind string, lister crdLister) ([]K8sObject, error) {
	var lastErr error
	for _, gvr := range candidateGVRs(apiGroup, kind) {
		list, err := lister.List(ctx, gvr, "")
		if err != nil {
			if isNoMatchOrNotFound(err) {
				lastErr = err
				continue // 候选 version 未命中，尝试下一个
			}
			return nil, err
		}
		out := make([]K8sObject, 0, len(list.Items))
		for i := range list.Items {
			item := list.Items[i]
			out = append(out, K8sObject{
				Namespace: item.GetNamespace(),
				Name:      item.GetName(),
				Content:   item.Object,
			})
		}
		return out, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("cert: crd %s/%s not found in cluster %q", apiGroup, kind, cluster)
	}
	return nil, fmt.Errorf("cert: no gvr candidates for %s/%s in cluster %q", apiGroup, kind, cluster)
}

// isNoMatchOrNotFound GVR 候选探测的"未命中"判定（其余错误透传）。
func isNoMatchOrNotFound(err error) bool {
	return k8smeta.IsNoMatchError(err) || apierrors.IsNotFound(err)
}

// candidateGVRs 登记项 → GVR 候选清单：内置固定枚举精确命中；
// 自定义登记取 plural=lower(kind)+"s" 与常见 version 候选。
func candidateGVRs(apiGroup, kind string) []schema.GroupVersionResource {
	for _, b := range k8s.BuiltinRegistrations {
		if b.APIGroup == apiGroup && b.Kind == kind {
			return []schema.GroupVersionResource{b.GVR()}
		}
	}
	resource := strings.ToLower(kind) + "s"
	versions := []string{"v1", "v1beta1", "v1alpha1"}
	out := make([]schema.GroupVersionResource, 0, len(versions))
	for _, v := range versions {
		out = append(out, schema.GroupVersionResource{Group: apiGroup, Version: v, Resource: resource})
	}
	return out
}

// accountScanSource 扫描账号源生产实现：account 仓储 active 账号
// （凭证解密在仓储读取路径完成；仅内存传递给云适配，禁入日志/错误）。
type accountScanSource struct {
	repo accountrepo.CloudAccountRepository
}

// NewAccountScanSource 创建扫描账号源。
func NewAccountScanSource(repo accountrepo.CloudAccountRepository) ScanAccountSource {
	return &accountScanSource{repo: repo}
}

// ActiveByCloud 返回指定云的全部 active 账号。
func (s *accountScanSource) ActiveByCloud(ctx context.Context, cloud domain.Cloud) ([]*sharedomain.CloudAccount, error) {
	accounts, _, err := s.repo.List(ctx, sharedomain.CloudAccountFilter{
		Provider: sharedomain.CloudProvider(cloud),
		Status:   sharedomain.CloudAccountStatusActive,
	})
	if err != nil {
		return nil, fmt.Errorf("cert: list active accounts for %s: %w", cloud, err)
	}
	out := make([]*sharedomain.CloudAccount, len(accounts))
	for i := range accounts {
		out[i] = &accounts[i]
	}
	return out, nil
}

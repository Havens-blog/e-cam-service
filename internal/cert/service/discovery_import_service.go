package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aliyun"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aws"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/azure"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/huawei"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/tencent"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// ---------------------------------------------------------------------
// 云端发现导入：会话编排（cert-cloud-discovery-import 任务 4）
// ---------------------------------------------------------------------

// discoveryImportTimeout 发现导入会话整体处理时限（对齐批量导入
// batchProcessTimeout 的 10 分钟口径防 goroutine 泄漏；超时后剩余条目不再调
// 云 API、逐条记 SESSION_TIMEOUT 失败因——条目幂等保证可重跑收敛）。
const discoveryImportTimeout = batchProcessTimeout

// discoveryRecordTimeout 结果记录/终态收敛预算：与处理时限解耦——整体超时后
// 剩余条目的失败落库与 MarkFinished 仍可用（会话不因超时永久卡 running）。
const discoveryRecordTimeout = time.Minute

// ErrEmptyDiscoveryImport 发现导入请求未携带任何条目。
var ErrEmptyDiscoveryImport = errors.New("cert: discovery import contains no items")

// 发现导入条目结果文案（Hard Rule：静态文案，不携带云响应片段与 panic 值；
// errorReason 在 result=success 时承载幂等重放说明，failed 时为错误码+静态文案）。
const (
	reasonDiscoveryUnsupportedCloud = "CERT_IMPORT_UNSUPPORTED: 该云证书暂不支持自动解析"
	reasonDiscoveryIAMHosted        = "CERT_IMPORT_UNSUPPORTED: IAM-hosted 证书暂不支持自动解析"
	reasonDiscoveryCertGone         = "CERT_GET_FAILED: 云侧已不存在"
	reasonDiscoveryNoPEM            = "CERT_GET_FAILED: 云侧未返回可导入的证书材料"
	reasonDiscoveryGetCertFailed    = "CERT_GET_FAILED: 云证书读取失败"
	reasonDiscoveryAccountMissing   = "ACCOUNT_NOT_FOUND: 云账号不存在或未启用"
	reasonDiscoveryAccountLoadFail  = "ACCOUNT_LOAD_FAILED: 云账号读取失败"
	reasonDiscoveryLedgerFail       = "INTERNAL_ERROR: 台账写入失败"
	reasonDiscoveryPanic            = "INTERNAL_ERROR: unexpected failure during import"
	reasonDiscoveryTimeout          = "SESSION_TIMEOUT: 会话整体超时，条目可重跑"
	reasonDiscoveryAlreadyInLedger  = "ALREADY_IN_LEDGER: 已在台账，已补建映射"
)

// DiscoveryImportItemInput 发现导入请求单条目（预览勾选条目，web 层请求体映射；
// 三元组定位与 DiscoveryImportItem 一致）。
type DiscoveryImportItemInput struct {
	Cloud       string
	AccountKey  string
	CloudCertID string
}

// DiscoveryImportService 云端发现导入编排（任务 4）：会话先持久化再异步执行
// （浏览器中断不丢结果），逐条 GetCert（净化 PEM）→解析→指纹登记
// （fingerprint_only）→CloudCertMapping 幂等建档→占位指纹引用回填；
// 单条失败/panic 不中断会话（Hard Rule），终态按失败计数收敛
// completed/partial_failed。云凭证按账号在会话内解析（会话生命周期长于
// HTTP 请求，复用扫描链路 ScanAccountSource.ActiveByCloud 模式）。
type DiscoveryImportService interface {
	// ImportFromDiscovery 创建发现导入会话（status=running、items=pending）
	// 并异步逐条处理，返回会话 ID（hex）。空清单返回 ErrEmptyDiscoveryImport。
	ImportFromDiscovery(ctx context.Context, items []DiscoveryImportItemInput, operator string) (string, error)
	// GetSession 会话进度轮询数据源（任务 5 GET 端点）。
	GetSession(ctx context.Context, sessionID string) (domain.DiscoveryImportSession, error)
}

type discoveryImportService struct {
	sessions domain.DiscoveryImportSessionRepository
	certs    domain.CertificateRepository
	mappings domain.CloudCertMappingRepository
	refs     domain.CertReferenceRepository
	adapters map[domain.Cloud]DiscoveryCertAdapter
	accounts ScanAccountSource
}

// NewDiscoveryImportService 创建发现导入服务；adapters 为逐云证书材料端口
// （生产装配见各 NewXxxDiscoveryCertAdapter），未注册的云（如华为云）条目按
// 不支持口径记因跳过。
func NewDiscoveryImportService(
	sessions domain.DiscoveryImportSessionRepository,
	certs domain.CertificateRepository,
	mappings domain.CloudCertMappingRepository,
	refs domain.CertReferenceRepository,
	adapters []DiscoveryCertAdapter,
	accounts ScanAccountSource,
) DiscoveryImportService {
	byCloud := make(map[domain.Cloud]DiscoveryCertAdapter, len(adapters))
	for _, a := range adapters {
		byCloud[a.Cloud()] = a
	}
	return &discoveryImportService{
		sessions: sessions,
		certs:    certs,
		mappings: mappings,
		refs:     refs,
		adapters: byCloud,
		accounts: accounts,
	}
}

// GetSession 会话查询（进度轮询端点数据源，任务 5 接线）。
func (s *discoveryImportService) GetSession(ctx context.Context, sessionID string) (domain.DiscoveryImportSession, error) {
	return s.sessions.GetByID(ctx, sessionID)
}

// ImportFromDiscovery 先持久化会话（浏览器中断后重开仍可见结果），再异步逐条
// 处理。返回会话 ID 即 cert_discovery_import_sessions._id。
func (s *discoveryImportService) ImportFromDiscovery(ctx context.Context, items []DiscoveryImportItemInput, operator string) (string, error) {
	if len(items) == 0 {
		return "", ErrEmptyDiscoveryImport
	}
	session := &domain.DiscoveryImportSession{
		Items:    make([]domain.DiscoveryImportItem, len(items)),
		Progress: domain.DiscoveryImportProgress{Total: len(items)},
		Operator: operator,
	}
	for i, it := range items {
		session.Items[i] = domain.DiscoveryImportItem{
			Cloud:       it.Cloud,
			AccountKey:  it.AccountKey,
			CloudCertID: it.CloudCertID,
			Result:      domain.DiscoveryItemPending,
		}
	}
	sessionID, err := s.sessions.Create(ctx, session)
	if err != nil {
		return "", err
	}

	// 会话处理不随请求生命周期终止（浏览器中断不影响）；整体限时防泄漏。
	go s.runImport(sessionID, session.Items)
	return sessionID, nil
}

// runImport 逐条处理会话：单条失败/panic 落 errorReason 后继续（Hard Rule），
// 全部完成后按失败计数收敛终态（completed / partial_failed，由仓储以库内
// progress.failed 判定）。整体限时到期后剩余条目不调云 API、逐条记超时失败因。
func (s *discoveryImportService) runImport(sessionID string, items []domain.DiscoveryImportItem) {
	ctx, cancel := context.WithTimeout(context.Background(), discoveryImportTimeout)
	defer cancel()
	recCtx, recCancel := context.WithTimeout(context.Background(), discoveryRecordTimeout)
	defer recCancel()

	run := &discoveryImportRun{
		accounts:   s.accounts,
		byCloud:    make(map[domain.Cloud][]*sharedomain.CloudAccount),
		errByCloud: make(map[domain.Cloud]error),
	}
	for i := range items {
		if ctx.Err() != nil {
			// 整体时限已过：不再调云 API；剩余条目记因可重跑（条目幂等）
			_ = s.sessions.RecordItemResult(recCtx, sessionID, i, domain.DiscoveryItemFailed, "", reasonDiscoveryTimeout)
			continue
		}
		s.processItem(ctx, recCtx, sessionID, i, items[i], run)
	}
	if err := s.sessions.MarkFinished(recCtx, sessionID); err != nil {
		slog.Error("cert discovery import: mark finished failed",
			slog.String("sessionId", sessionID), slog.Any("err", err))
	}
}

// processItem 处理单条目并记录结果；recover 兜底保证 panic 不中断会话
// （Hard Rule；静态文案，不携带 panic 值）。
func (s *discoveryImportService) processItem(
	ctx, recCtx context.Context,
	sessionID string, idx int,
	item domain.DiscoveryImportItem,
	run *discoveryImportRun,
) {
	defer func() {
		if r := recover(); r != nil {
			// 静态文案，不携带 panic 值（可能含任意堆数据）
			slog.Error("cert discovery import: panic during item import",
				slog.String("sessionId", sessionID),
				slog.String("cloud", item.Cloud), slog.String("cloudCertId", item.CloudCertID))
			_ = s.sessions.RecordItemResult(recCtx, sessionID, idx, domain.DiscoveryItemFailed, "", reasonDiscoveryPanic)
		}
	}()

	fail := func(reason string) {
		_ = s.sessions.RecordItemResult(recCtx, sessionID, idx, domain.DiscoveryItemFailed, "", reason)
	}

	// 1. 支持性预检（不调云 API，与预览 parseable 口径一致）：华为云无 PEM
	//    通道（SHA-1 口径）、AWS IAM-hosted（非 ARN 形态 ID）
	switch {
	case item.Cloud == string(domain.CloudHuawei):
		fail(reasonDiscoveryUnsupportedCloud)
		return
	case item.Cloud == string(domain.CloudAWS) && !strings.HasPrefix(item.CloudCertID, "arn:"):
		fail(reasonDiscoveryIAMHosted)
		return
	}
	adapter := s.adapters[domain.Cloud(item.Cloud)]
	if adapter == nil {
		fail(reasonDiscoveryUnsupportedCloud) // 未注册材料端口的云同口径跳过
		return
	}

	// 2. 云账号凭证（会话内按账号解析；读取失败/账号缺失逐条记因不中断）
	creds, err := run.accountFor(ctx, adapter.Cloud(), item.AccountKey)
	if err != nil {
		fail(reasonDiscoveryAccountLoadFail)
		return
	}
	if creds == nil {
		fail(reasonDiscoveryAccountMissing)
		return
	}

	// 3. GetCert 材料通道（任务 1：适配层已构造性净化为仅 CERTIFICATE 块序列）
	info, err := adapter.GetCertChain(ctx, creds, item.CloudCertID)
	if err != nil {
		if errors.Is(err, cloudx.ErrCertPEMUnsupported) {
			fail(reasonDiscoveryUnsupportedCloud) // 防御性兜底（预检已拦截常见形态）
			return
		}
		// 静态文案：云侧错误细节只进日志，不进 errorReason（Hard Rule）
		slog.Error("cert discovery import: get cert failed",
			slog.String("sessionId", sessionID),
			slog.String("cloud", item.Cloud), slog.String("cloudCertId", item.CloudCertID),
			slog.Any("err", err))
		fail(reasonDiscoveryGetCertFailed)
		return
	}
	if !info.Exists {
		fail(reasonDiscoveryCertGone) // 预览后云侧删除漂移
		return
	}
	if info.CertChainPEM == "" {
		fail(reasonDiscoveryNoPEM) // 在库但未返回 PEM（如 Azure 非证书 secret）
		return
	}
	// 防御性二次净化（幂等）：端口语义在适配层已构造性保证，边界再净一次使
	// "仅 CERTIFICATE 块入台账"不依赖远端契约（私钥不落库 Hard Rule）
	certPEM := cloudx.SanitizeCertChainPEM([]byte(info.CertChainPEM))
	if certPEM == "" {
		fail(reasonDiscoveryNoPEM)
		return
	}

	// 4. 解析校验（盘点容忍模式：过期/缺链不拦截，返回 MaterialIssue 标记
	//    留痕台账——"云侧有什么就登记什么"；结构异常仍拒绝）
	parsed, materialIssue, err := domain.ParseCertForInventory([]byte(certPEM))
	if err != nil {
		fail(discoveryParseReason(err)) // 域内解析错误为自有静态文案（安全参数）
		return
	}

	// 5. 指纹登记（fingerprint_only：EncryptedPrivateKey 缺省——导入路径仅写
	//    CertPEM/指纹及解析字段，无私钥材料可写）
	cert := &domain.Certificate{
		Fingerprint:   parsed.Fingerprint,
		CommonName:    parsed.CommonName,
		Sans:          parsed.Sans,
		Issuer:        parsed.Issuer,
		SerialNumber:  parsed.SerialNumber,
		NotBefore:     parsed.NotBefore,
		NotAfter:      parsed.NotAfter,
		KeyAlgorithm:  parsed.KeyAlgorithm,
		MaterialIssue: materialIssue,
		CertPEM:       certPEM,
		HostingStatus: domain.HostingStatusFingerprintOnly,
	}
	certID := ""
	alreadyInLedger := false
	if err := s.certs.Create(ctx, cert); err != nil {
		if !errors.Is(err, domain.ErrDuplicateFingerprint) {
			slog.Error("cert discovery import: create certificate failed",
				slog.String("sessionId", sessionID), slog.Any("err", err))
			fail(reasonDiscoveryLedgerFail)
			return
		}
		// SC-5 幂等重放：同指纹不产生重复台账记录——取既有证书、继续补建
		// 本云本账号映射、条目记 success（多账号场景不因此降级）
		existing, err := s.certs.GetByFingerprint(ctx, parsed.Fingerprint)
		if err != nil {
			slog.Error("cert discovery import: load existing certificate failed",
				slog.String("sessionId", sessionID), slog.Any("err", err))
			fail(reasonDiscoveryLedgerFail)
			return
		}
		certID = existing.ID.Hex()
		alreadyInLedger = true
	} else {
		certID = cert.ID.Hex()
	}

	// 6. CloudCertMapping 幂等建档（uk_fp_cloud_account 两段去重）
	if err := s.mappings.Upsert(ctx, &domain.CloudCertMapping{
		CertFingerprint: parsed.Fingerprint,
		Cloud:           item.Cloud,
		AccountKey:      item.AccountKey,
		CloudCertID:     item.CloudCertID,
	}); err != nil {
		slog.Error("cert discovery import: upsert mapping failed",
			slog.String("sessionId", sessionID), slog.Any("err", err))
		fail(reasonDiscoveryLedgerFail)
		return
	}

	// 7. 占位指纹引用回填（SC-6，best-effort 补偿写）：按 (cloud,accountKey,
	//    cloudCertId) 将仍为占位公式派生值的引用批量回填为真实指纹；filter 含
	//    占位值构成 CAS（真实指纹引用永不被覆盖），失败仅记日志不失败条目
	//    ——重跑命中幂等重放路径会再次回填，误回填可由重扫按公式重建。
	placeholder := placeholderFingerprintFor(item.Cloud, item.AccountKey, item.CloudCertID)
	if n, err := s.refs.BackfillFingerprint(ctx, item.Cloud, item.AccountKey, item.CloudCertID, placeholder, parsed.Fingerprint); err != nil {
		slog.Error("cert discovery import: backfill references failed",
			slog.String("sessionId", sessionID), slog.Any("err", err))
	} else if n > 0 {
		slog.Info("cert discovery import: backfilled placeholder references",
			slog.String("sessionId", sessionID),
			slog.String("cloud", item.Cloud), slog.String("cloudCertId", item.CloudCertID),
			slog.Int64("count", n))
	}

	note := ""
	if alreadyInLedger {
		note = reasonDiscoveryAlreadyInLedger
	}
	if issueNote := materialIssueNote(materialIssue); issueNote != "" {
		// 盘点容忍标记附注（新登记时提示材料异常；重放补映射时与已有注记拼接）
		if note == "" {
			note = issueNote
		} else {
			note = note + "；" + issueNote
		}
	}
	_ = s.sessions.RecordItemResult(recCtx, sessionID, idx, domain.DiscoveryItemSuccess, certID, note)
}

// materialIssueNote 盘点容忍标记的条目附注（静态文案）。
func materialIssueNote(issue domain.MaterialIssue) string {
	switch issue {
	case domain.MaterialIssueExpired:
		return "MATERIAL_ISSUE: 已登记（材料异常：已过期）"
	case domain.MaterialIssueChainIncomplete:
		return "MATERIAL_ISSUE: 已登记（材料异常：证书链不完整）"
	default:
		return ""
	}
}

// discoveryParseReason 解析/校验失败原因：域内 CertError 为自有静态文案
// （时间/算法名等安全参数），携带码+完整链文案（err.Error 保留 wrapped 细节，
// 如 expired at <日期>/缺自签根——运营区分过期与结构异常的判据）；
// 其余归 INTERNAL_ERROR 静态码。
func discoveryParseReason(err error) string {
	if ce, ok := domain.AsCertError(err); ok {
		return ce.Code() + ": " + err.Error()
	}
	return "INTERNAL_ERROR: 证书解析失败"
}

// discoveryImportRun 单次导入会话运行态：云账号凭证按云惰性缓存
// （会话生命周期长于 HTTP 请求，凭证需在会话内按账号获取——ActiveByCloud
// 模式；同一会话内同云只读一次账号表）。
type discoveryImportRun struct {
	accounts   ScanAccountSource
	byCloud    map[domain.Cloud][]*sharedomain.CloudAccount
	errByCloud map[domain.Cloud]error
}

// accountFor 解析 (cloud, accountKey) 的云账号凭证；账号未命中返回 (nil, nil)
// （调用方记 ACCOUNT_NOT_FOUND），读取失败返回错误（同云错误缓存，本会话内
// 该云条目共享失败口径）。
func (r *discoveryImportRun) accountFor(ctx context.Context, cloud domain.Cloud, accountKey string) (*sharedomain.CloudAccount, error) {
	accounts, err := r.accountsFor(ctx, cloud)
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		if a.Name == accountKey { // accountKey 口径 = CloudAccount.Name（与各云适配 AccountKey 同源）
			return a, nil
		}
	}
	return nil, nil
}

// accountsFor 按云惰性加载 active 账号（含错误缓存）。
func (r *discoveryImportRun) accountsFor(ctx context.Context, cloud domain.Cloud) ([]*sharedomain.CloudAccount, error) {
	if err, ok := r.errByCloud[cloud]; ok {
		return nil, err
	}
	if accounts, ok := r.byCloud[cloud]; ok {
		return accounts, nil
	}
	accounts, err := r.accounts.ActiveByCloud(ctx, cloud)
	if err != nil {
		r.errByCloud[cloud] = err
		return nil, err
	}
	r.byCloud[cloud] = accounts
	return accounts, nil
}

// ---------------------------------------------------------------------
// 证书材料端口与生产适配 shim（任务 1 四云 CertChainPEM 通道 → 导入端口）
// ---------------------------------------------------------------------

// DiscoveryCertMaterial GetCert 材料通道返回形态：在库状态 + 净化证书序列。
type DiscoveryCertMaterial struct {
	Exists       bool   // 云证书库中该 cloudCertId 是否存在
	CertChainPEM string // 仅 CERTIFICATE 块的净化序列（叶在前 fullchain，适配层构造性净化）
}

// DiscoveryCertAdapter 单云证书材料端口（发现导入专用，只读）：区别于扫描端口
// 的 CloudCertStatus 形态（无 PEM）——导入路径需要证书材料落台账，经任务 1
// 的 CertChainPEM 通道承载。逐证限速由各适配器 waitRateLimit 内部保证。
type DiscoveryCertAdapter interface {
	// Cloud 适配归属云（aliyun|tencent|huawei|aws|azure）。
	Cloud() domain.Cloud
	// GetCertChain 读取云侧证书材料（净化 PEM）；华为云/IAM-hosted 形态返回
	// cloudx.ErrCertPEMUnsupported 降级哨兵（预检拦截外的防御性兜底）。
	GetCertChain(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (DiscoveryCertMaterial, error)
}

// discoveryCertAdapter 通用 shim：cloud 元数据 + GetCert 闭包。
type discoveryCertAdapter struct {
	cloud    domain.Cloud
	getChain func(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (DiscoveryCertMaterial, error)
}

func (a discoveryCertAdapter) Cloud() domain.Cloud { return a.cloud }

func (a discoveryCertAdapter) GetCertChain(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (DiscoveryCertMaterial, error) {
	return a.getChain(ctx, creds, cloudCertID)
}

// NewAliyunDiscoveryCertAdapter 阿里云导入材料适配（CAS GetUserCertificateDetail 通道）。
func NewAliyunDiscoveryCertAdapter(a *aliyun.CertAdapter) DiscoveryCertAdapter {
	return discoveryCertAdapter{
		cloud: domain.CloudAliyun,
		getChain: func(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (DiscoveryCertMaterial, error) {
			info, err := a.GetCert(ctx, creds, cloudCertID)
			return DiscoveryCertMaterial{Exists: info.Exists, CertChainPEM: info.CertChainPEM}, err
		},
	}
}

// NewTencentDiscoveryCertAdapter 腾讯云导入材料适配（SSL DescribeCertificateDetail
// 通道；SHA-1 口径回退条目导入时由 PEM 解析补全指纹）。
func NewTencentDiscoveryCertAdapter(a *tencent.CertAdapter) DiscoveryCertAdapter {
	return discoveryCertAdapter{
		cloud: domain.CloudTencent,
		getChain: func(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (DiscoveryCertMaterial, error) {
			info, err := a.GetCert(ctx, creds, cloudCertID)
			return DiscoveryCertMaterial{Exists: info.Exists, CertChainPEM: info.CertChainPEM}, err
		},
	}
}

// NewHuaweiDiscoveryCertAdapter 华为云导入材料适配（SCM 无 PEM 通道，GetCert 恒
// 返回 ErrCertPEMUnsupported——条目在服务层预检即记因跳过，本适配仅为五云对称
// 装配兜底，不发起云 API 调用；其 CloudCertInfo 无 CertChainPEM 字段，材料恒空）。
func NewHuaweiDiscoveryCertAdapter(a *huawei.CertDiscoveryAdapter) DiscoveryCertAdapter {
	return discoveryCertAdapter{
		cloud: domain.CloudHuawei,
		getChain: func(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (DiscoveryCertMaterial, error) {
			_, err := a.GetCert(ctx, creds, cloudCertID) // 恒 ErrCertPEMUnsupported
			return DiscoveryCertMaterial{}, err
		},
	}
}

// NewAwsDiscoveryCertAdapter AWS 导入材料适配（ACM GetCertificate 叶在前拼装通道；
// IAM-hosted 非 ARN 形态返回 ErrCertPEMUnsupported——服务层预检拦截）。
func NewAwsDiscoveryCertAdapter(a *aws.CertDiscoveryAdapter) DiscoveryCertAdapter {
	return discoveryCertAdapter{
		cloud: domain.CloudAWS,
		getChain: func(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (DiscoveryCertMaterial, error) {
			info, err := a.GetCert(ctx, creds, cloudCertID)
			return DiscoveryCertMaterial{Exists: info.Exists, CertChainPEM: info.CertChainPEM}, err
		},
	}
}

// NewAzureDiscoveryCertAdapter Azure 导入材料适配（KeyVault GetSecret 通道，
// secret 全量值经适配层构造性净化）。
func NewAzureDiscoveryCertAdapter(a *azure.CertDiscoveryAdapter) DiscoveryCertAdapter {
	return discoveryCertAdapter{
		cloud: domain.CloudAzure,
		getChain: func(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (DiscoveryCertMaterial, error) {
			info, err := a.GetCert(ctx, creds, cloudCertID)
			return DiscoveryCertMaterial{Exists: info.Exists, CertChainPEM: info.CertChainPEM}, err
		},
	}
}

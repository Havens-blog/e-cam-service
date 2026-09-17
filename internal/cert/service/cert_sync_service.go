package service

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	volcanocert "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/volcano"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------------------------------------------------------------------
// 多云定时增量同步服务（cert-volcano-import-sync 任务 3）
// ---------------------------------------------------------------------

// 云证书库是台账唯一真源：同步服务把"云端证书库对齐台账"变成定时纪律——
// 枚举全部证书可达云 × active 账号 → 适配器列举 → 与「台账指纹 + 现有映射」
// 比对增量处理（未入账指纹才转导入管线、已映射跳过、映射缺失/漂移补刷）。
//
// Hard Rules：
//   - 只读纪律：同步路径只经只读窄端口调云侧列举（List），导入路径复用发现
//     导入管线的 GetCertChain 读通道——任何云写方法不在端口面内（构造性保证），
//     绝不触发任何线上变更（替换走变更清单，Out of Scope）；
//   - 整体限时复用 discoveryImportTimeout 语义：到期剩余云/条目记超时失败因，
//     不悬挂（条目幂等保证可重跑收敛）；
//   - 不修改 mapping 表结构、不引入新幂等机制：增量判定/补建/漂移全部复用
//     既有仓储语义（uk_fingerprint 指纹唯一键 + uk_fp_cloud_account 映射
//     Upsert + FindByCloudCertID uploadedAt 降序）。
//
// 增量判定口径（任务提示修正，与 proposal 增量段一致）：以列举层元数据
// （cloudCertID/指纹）与「台账指纹 + 现有映射」比对，命中即 skip——判定层
// 不发起任何云 Get；列举适配器若在 List 内部逐实例拉取（火山 SDK List 无
// 指纹字段），该 Get 成本为适配器层固有约束，同步层的跳过=不产生导入动作
// 与台账写。

// ErrSyncRunning 同步轮已在执行（CAS 防重守卫；running 中再次触发不启动
// 第二轮——定时轮与手工触发共享同一守卫，任务 4/5 消费）。
var ErrSyncRunning = errors.New("cert: cert sync already running")

// 同步会话 operator 标识（会话来源可观测：定时轮/手工触发）。
const (
	syncOperatorScheduler = "scheduler"
	syncOperatorManual    = "manual"
)

// 同步轮失败文案（Hard Rule：静态错误码+静态文案，云侧错误细节只进日志）。
const (
	reasonSyncAccountFailed = "ACCOUNT_LOAD_FAILED: 云账号读取失败"
	reasonSyncListFailed    = "CERT_LIST_FAILED: 云证书库列举失败"
	reasonSyncJudgeFailed   = "INTERNAL_ERROR: 增量判定失败"
	reasonSyncMappingFailed = "INTERNAL_ERROR: 映射补建失败"
	reasonSyncTimeout       = "SESSION_TIMEOUT: 同步整体超时，剩余条目可重跑"
	reasonSyncSubmitFailed  = "INTERNAL_ERROR: 导入会话创建失败"
)

// certSyncClouds 证书可达云全集（固定遍历顺序保证运行可复现）：五云 +
// 火山（账号 provider 枚举同口径，火山经 discoveryCloudVolcano 转译）。
var certSyncClouds = []domain.Cloud{
	domain.CloudAliyun,
	domain.CloudTencent,
	domain.CloudHuawei,
	domain.CloudAWS,
	domain.CloudAzure,
	discoveryCloudVolcano,
}

// CertLibraryInstance 云证书库在库实例元数据（增量判定的最小输入面）：
// 指纹与台账聚合主键同构（SHA256(leaf.Raw) 小写 hex）；指纹为空表示该云
// 列举层无法复核指纹（SHA-1 口径等降级形态），仅能按映射通道判定。
type CertLibraryInstance struct {
	CloudCertID string
	Fingerprint string
}

// CertLibraryLister 单云证书库只读列举端口（同步服务专用）：与发现导入材料
// 端口（DiscoveryCertAdapter）正交——列举供增量判定，材料通道供导入。
// 窄接口仅一个只读方法，不含任何写通路（Hard Rule 构造性保证）。
type CertLibraryLister interface {
	// Cloud 适配归属云（aliyun|tencent|huawei|aws|azure|volcano）。
	Cloud() domain.Cloud
	// ListInstances 列举该账号证书库全部在库实例（revoked/非已签发等过滤
	// 态由各云适配器按签发语义前置过滤，不进入返回集）。
	ListInstances(ctx context.Context, creds *sharedomain.CloudAccount) ([]CertLibraryInstance, error)
}

// ---------------------------------------------------------------------
// 火山证书库列举端口 shim（任务 1 cloudx 适配器 → 同步服务端口）
// ---------------------------------------------------------------------

// 编译期断言：任务 1 火山证书库适配器满足列举端口消费形态（窄面仅列举一个
// 读方法——GetCertificate 等其余面不经本端口暴露，材料通道归导入管线）。
var _ volcanoCertLister = (*volcanocert.CertAdapter)(nil)

// volcanoCertLister 列举端口消费的火山适配器形态（窄接口：仅列举一个读方法）。
type volcanoCertLister interface {
	ListCertificates(ctx context.Context, creds *sharedomain.CloudAccount) ([]volcanocert.CloudCertInstance, error)
}

// volcanoCertLibraryLister 火山证书库列举适配（包装 cloudx/volcano CertAdapter
// 的 ListCertificates——适配器已含分页、revoked/非 Issued 过滤与链解析）。
// 包装层仅做端口形态适配，不复制云侧逻辑（Hard Rule）。
type volcanoCertLibraryLister struct {
	adapter volcanoCertLister
}

func (l volcanoCertLibraryLister) Cloud() domain.Cloud { return discoveryCloudVolcano }

func (l volcanoCertLibraryLister) ListInstances(ctx context.Context, creds *sharedomain.CloudAccount) ([]CertLibraryInstance, error) {
	instances, err := l.adapter.ListCertificates(ctx, creds)
	return volcanoListInstances(instances, err)
}

// volcanoListInstances 火山实例 → 列举端口形态（端口适配映射，纯函数）：
// 空 cloudCertID/指纹的实例防御性剔除（无三元组/无法指纹判定的实例不进判定）。
func volcanoListInstances(instances []volcanocert.CloudCertInstance, err error) ([]CertLibraryInstance, error) {
	if err != nil {
		return nil, err
	}
	out := make([]CertLibraryInstance, 0, len(instances))
	for _, in := range instances {
		if in.CloudCertID == "" || in.Fingerprint == "" {
			continue
		}
		out = append(out, CertLibraryInstance{CloudCertID: in.CloudCertID, Fingerprint: in.Fingerprint})
	}
	return out, nil
}

// NewVolcanoCertLibraryLister 火山引擎证书库列举适配（cert-volcano-import-sync
// 任务 3；云侧逻辑全部在任务 1 适配器内）：service.NewVolcanoCertLibraryLister(
// volcanocert.NewCertAdapter(logger))。
func NewVolcanoCertLibraryLister(a *volcanocert.CertAdapter) CertLibraryLister {
	return volcanoCertLibraryLister{adapter: a}
}

// ---------------------------------------------------------------------
// 服务
// ---------------------------------------------------------------------

// SyncFailure 同步轮单条失败摘要（静态错误码；云侧错误细节不进摘要只进日志，
// 任务 5 端点响应直接以此承载"仅错误类别/reason 摘要"）。
type SyncFailure struct {
	Cloud       string // 失败归属云；空=跨云聚合（整体超时等）
	AccountKey  string // 失败归属账号；空=云级失败
	CloudCertID string // 判定层条目失败时有值；云/账号级失败为空
	Reason      string // 静态错误码+文案
}

// SyncRun 一轮多云证书同步的结果摘要（任务 5 端点响应数据源；计数语义：
// Listed = 枚举到的实例总数；Skipped/Backfilled/Drifted/Imported 为增量
// 判定四态；ImportSucceeded/ImportFailed 为导入会话终态计数）。
type SyncRun struct {
	SessionID  string                       // 导入会话 ID（无导入条目为空串）
	Status     domain.DiscoveryImportStatus // completed / partial_failed（会话终态同词汇）
	StartedAt  time.Time
	FinishedAt time.Time

	CloudsScanned   int // 参与枚举的云数（有列举端口且账号读取成功）
	AccountsScanned int // 枚举的 (cloud, account) 对数
	Listed          int // 列举返回的实例总数

	Skipped    int // 已映射跳过（台账指纹 + 映射完整；不产生导入与台账写）
	Backfilled int // 映射缺失补建（指纹已在台账）
	Drifted    int // 同 cloudCertID 新指纹刷新（旧映射留痕）
	Imported   int // 未入账指纹转导入管线的条目数

	ImportSucceeded int // 导入会话 success 条数（含 ALREADY_IN_LEDGER 幂等重放）
	ImportFailed    int // 导入会话 failed 条数

	Failures []SyncFailure // 枚举/判定层失败清单（静态错误码；导入层失败经会话条目承载）
}

// fail 记录一条失败（cloud 为 domain.Cloud 零值时存空串=跨云聚合）。
func (r *SyncRun) fail(cloud domain.Cloud, accountKey, cloudCertID, reason string) {
	r.Failures = append(r.Failures, SyncFailure{
		Cloud:       string(cloud),
		AccountKey:  accountKey,
		CloudCertID: cloudCertID,
		Reason:      reason,
	})
}

// CertSyncService 多云定时增量同步服务：定时轮（任务 4 调度点窄端口）与手工
// 触发（任务 5 端点）共用的核心。同步执行（调用返回即本轮终态），CAS 防重
// 守卫保证任一时刻至多一轮在跑。
type CertSyncService interface {
	// SyncCertificates 执行一轮同步（operator=scheduler；定时轮入口）。
	// 已在跑返回 ErrSyncRunning。
	SyncCertificates(ctx context.Context) (SyncRun, error)
	// SyncCertificatesManual 执行一轮同步（operator=manual；任务 5 手工触发
	// 入口，与定时轮共享同一 CAS 守卫）。已在跑返回 ErrSyncRunning。
	SyncCertificatesManual(ctx context.Context) (SyncRun, error)
}

// certSyncService 同步服务实现。
type certSyncService struct {
	running        atomic.Bool // CAS 防重守卫（对齐 probe probeRunning 模式）
	overallTimeout time.Duration

	listers  map[domain.Cloud]CertLibraryLister
	accounts ScanAccountSource
	importer DiscoveryImportService // 既有发现导入幂等管线（同步执行面）
	certs    domain.CertificateRepository
	mappings domain.CloudCertMappingRepository
}

// NewCertSyncService 创建同步服务：listers 为逐云证书库列举端口（生产装配
// 当前仅火山具备证书库列举能力——五云接入列举端口后即插即用，未注册的云
// 跳过不报错）；importer 为既有发现导入服务（未入账指纹经其幂等管线入账）。
func NewCertSyncService(
	listers []CertLibraryLister,
	accounts ScanAccountSource,
	importer DiscoveryImportService,
	certs domain.CertificateRepository,
	mappings domain.CloudCertMappingRepository,
) CertSyncService {
	byCloud := make(map[domain.Cloud]CertLibraryLister, len(listers))
	for _, l := range listers {
		byCloud[l.Cloud()] = l
	}
	return &certSyncService{
		overallTimeout: discoveryImportTimeout, // Hard Rule：整体限时同导入管线口径
		listers:        byCloud,
		accounts:       accounts,
		importer:       importer,
		certs:          certs,
		mappings:       mappings,
	}
}

// SyncCertificates 定时轮入口（operator=scheduler）。
func (s *certSyncService) SyncCertificates(context.Context) (SyncRun, error) {
	return s.run(syncOperatorScheduler)
}

// SyncCertificatesManual 手工触发入口（operator=manual）。
func (s *certSyncService) SyncCertificatesManual(context.Context) (SyncRun, error) {
	return s.run(syncOperatorManual)
}

// run 执行一轮同步。CAS 防重：任一时刻至多一轮在跑（二次触发返回
// ErrSyncRunning，不排队）。
//
// 限时口径：整体限时脱离调用方 ctx（定时轮无请求生命周期；手工触发的请求
// 中断不回滚已进行的导入——与发现导入管线"先持久化后处理"口径一致），到期
// 剩余云/条目记超时失败因后收敛返回，不悬挂。
func (s *certSyncService) run(operator string) (SyncRun, error) {
	if !s.running.CompareAndSwap(false, true) {
		return SyncRun{}, ErrSyncRunning
	}
	defer s.running.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), s.overallTimeout)
	defer cancel()
	recCtx, recCancel := context.WithTimeout(context.Background(), discoveryRecordTimeout)
	defer recCancel()

	run := SyncRun{
		StartedAt: time.Now(),
		Failures:  []SyncFailure{},
	}
	var importItems []DiscoveryImportItemInput

	// 1. 枚举证书可达云 × active 账号 → 列举 → 增量判定（单云单账号失败隔离）
	for _, cloud := range certSyncClouds {
		if ctx.Err() != nil {
			run.fail("", "", "", reasonSyncTimeout) // 剩余云整体记超时失败因
			break
		}
		lister := s.listers[cloud]
		if lister == nil {
			// 该云暂无证书库列举端口：能力缺口非故障（如五云待接入），跳过记日志
			slog.Debug("cert sync: no cert library lister, skip cloud",
				slog.String("cloud", string(cloud)))
			continue
		}
		accounts, err := s.accounts.ActiveByCloud(ctx, cloud)
		if err != nil {
			// 云侧错误细节只进日志（Hard Rule）
			slog.Error("cert sync: load active accounts failed",
				slog.String("cloud", string(cloud)), slog.Any("err", err))
			run.fail(cloud, "", "", reasonSyncAccountFailed)
			continue
		}
		run.CloudsScanned++
		for _, acct := range accounts {
			if ctx.Err() != nil {
				run.fail(cloud, acct.Name, "", reasonSyncTimeout)
				continue
			}
			s.syncAccount(ctx, &run, lister, cloud, acct, &importItems)
		}
	}

	// 2. 未入账指纹 → 既有幂等管线入账（会话 operator 标识来源；同步执行，
	//    单条失败/panic 隔离与终态收敛全复用管线语义）
	if len(importItems) > 0 {
		run.Imported = len(importItems)
		sessionID, err := s.importer.ImportFromDiscoverySync(recCtx, importItems, operator)
		if err != nil {
			slog.Error("cert sync: create import session failed", slog.Any("err", err))
			run.fail("", "", "", reasonSyncSubmitFailed)
		} else {
			run.SessionID = sessionID
			if sess, err := s.importer.GetSession(recCtx, sessionID); err != nil {
				slog.Error("cert sync: read back import session failed", slog.Any("err", err))
			} else {
				run.ImportSucceeded = sess.Progress.Succeeded
				run.ImportFailed = sess.Progress.Failed
			}
		}
	}

	run.FinishedAt = time.Now()
	if run.ImportFailed > 0 || len(run.Failures) > 0 {
		run.Status = domain.DiscoveryImportPartialFailed
	} else {
		run.Status = domain.DiscoveryImportCompleted
	}
	return run, nil
}

// syncAccount 单账号列举 + 逐实例判定：列举失败逐账号记因隔离（部分实例仍
// 判定——适配器契约：列举错误时返回已获取部分 + 错误，条目幂等可重跑收敛）。
func (s *certSyncService) syncAccount(
	ctx context.Context,
	run *SyncRun,
	lister CertLibraryLister,
	cloud domain.Cloud,
	acct *sharedomain.CloudAccount,
	importItems *[]DiscoveryImportItemInput,
) {
	instances, err := lister.ListInstances(ctx, acct)
	run.AccountsScanned++
	if err != nil {
		// 云侧错误细节只进日志（Hard Rule）；整体限时到期的列举失败按超时
		// 失败因记（可重跑语义），其余为列举失败
		slog.Error("cert sync: list cert library failed",
			slog.String("cloud", string(cloud)),
			slog.String("account", acct.Name), slog.Any("err", err))
		reason := reasonSyncListFailed
		if ctx.Err() != nil {
			reason = reasonSyncTimeout
		}
		run.fail(cloud, acct.Name, "", reason)
		if len(instances) == 0 {
			return
		}
	}
	run.Listed += len(instances)
	for _, inst := range instances {
		if ctx.Err() != nil {
			run.fail(cloud, acct.Name, inst.CloudCertID, reasonSyncTimeout)
			continue
		}
		s.judgeInstance(ctx, run, cloud, acct.Name, inst, importItems)
	}
}

// judgeInstance 单实例增量判定（recover 兜底保证 panic 不中断其余实例）：
//
//	指纹不在台账                      → 转导入条目（既有幂等管线）
//	指纹在台账 + 映射完整（同指纹）   → skip（判定层不发起云 Get——AC3）
//	指纹在台账 + 映射缺失             → Upsert 补建映射
//	指纹在台账 + 同 cloudCertID 换指纹 → Upsert 刷新（新指纹新行，旧行留痕）
func (s *certSyncService) judgeInstance(
	ctx context.Context,
	run *SyncRun,
	cloud domain.Cloud,
	accountKey string,
	inst CertLibraryInstance,
	importItems *[]DiscoveryImportItemInput,
) {
	defer func() {
		if r := recover(); r != nil {
			// 静态文案，不携带 panic 值
			slog.Error("cert sync: panic during instance judgment",
				slog.String("cloud", string(cloud)),
				slog.String("cloudCertId", inst.CloudCertID))
			run.fail(cloud, accountKey, inst.CloudCertID, reasonSyncJudgeFailed)
		}
	}()

	if inst.CloudCertID == "" {
		return // 无三元组定位的实例不进判定（列举层防御性剔除外的兜底）
	}

	m, err := s.mappings.FindByCloudCertID(ctx, string(cloud), accountKey, inst.CloudCertID)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		run.fail(cloud, accountKey, inst.CloudCertID, reasonSyncJudgeFailed)
		return
	}
	hasMapping := err == nil

	if inst.Fingerprint == "" {
		// 列举层无法复核指纹（SHA-1 口径等降级形态）：无法与台账指纹比对——
		// 已映射即视为收敛跳过；无映射转导入（管线解析真实指纹后补建映射）
		if hasMapping {
			run.Skipped++
			return
		}
		*importItems = append(*importItems, DiscoveryImportItemInput{
			Cloud: string(cloud), AccountKey: accountKey, CloudCertID: inst.CloudCertID,
		})
		return
	}

	if _, err := s.certs.GetByFingerprint(ctx, inst.Fingerprint); err != nil {
		if !errors.Is(err, mongo.ErrNoDocuments) {
			run.fail(cloud, accountKey, inst.CloudCertID, reasonSyncJudgeFailed)
			return
		}
		// 未入账指纹 → 导入条目（AC2）。同指纹跨云/跨账号多三元组各自成条目，
		// 管线 ErrDuplicateFingerprint 幂等归 success（AC5 语义）
		*importItems = append(*importItems, DiscoveryImportItemInput{
			Cloud: string(cloud), AccountKey: accountKey, CloudCertID: inst.CloudCertID,
		})
		return
	}

	// 指纹已在台账：映射完整 → skip（增量判定仅基于列举元数据与台账/映射，
	// 不再发起云 Get——AC3 修正口径）
	if hasMapping && m.CertFingerprint == inst.Fingerprint {
		run.Skipped++
		return
	}

	// 映射缺失补建 / 漂移刷新（AC4）：Upsert 唯一键 = (新指纹, cloud, accountKey)
	// 命中即写入；同 cloudCertID 旧指纹映射行不删（漂移留痕），FindByCloudCertID
	// 按 uploadedAt 降序取最新即新指纹（既有仓储换证语义，无新机制）
	if err := s.mappings.Upsert(ctx, &domain.CloudCertMapping{
		CertFingerprint: inst.Fingerprint,
		Cloud:           string(cloud),
		AccountKey:      accountKey,
		CloudCertID:     inst.CloudCertID,
	}); err != nil {
		run.fail(cloud, accountKey, inst.CloudCertID, reasonSyncMappingFailed)
		return
	}
	if hasMapping {
		run.Drifted++
	} else {
		run.Backfilled++
	}
}

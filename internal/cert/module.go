// Package cert 证书管理功能域（SSL 证书统一托管与更换）。
//
// 本文件为域模块装配入口（任务 7.1）：repository/service/deployer/scheduler/web
// 全量装配，经 ioc/cert.go 注入 Wire（Layer Placement：与 internal/cam 平级）。
// 仅做装配接线——业务逻辑分布在 service/deployer/scheduler 各文件（2.x~5.x 已实现）。
//
// 依赖方向硬约束（4.3）：internal/alert → internal/cert，本包永不反向 import
// alert——CertAlertPublisher 生产实现（webhook+email）由 ioc 装配层构造后注入。
package cert

import (
	"context"
	"fmt"
	"time"

	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	assetrepo "github.com/Havens-blog/e-cam-service/internal/asset/repository"
	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/k8s"
	"github.com/Havens-blog/e-cam-service/internal/cert/repository"
	"github.com/Havens-blog/e-cam-service/internal/cert/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/internal/cert/web"
	aliyuncert "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aliyun"
	awsdiscover "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aws"
	azurediscover "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/azure"
	huaweidiscover "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/huawei"
	tencentcert "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/tencent"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	"github.com/spf13/viper"
)

// Module 证书管理功能域模块。
type Module struct {
	Repos *repository.Repositories

	// 调度面（ioc InitCertJobs 消费；9 类定时任务入口，7.1）
	ScanSvc          service.ReferenceScanService
	InspectionJob    *scheduler.InspectionJob
	VerifyWindowSvc  service.VerifyWindowService
	ChangeSvc        service.ChangeService
	ExecuteSvc       service.ChangeExecuteService
	OrphanCleanupSvc service.OrphanCleanupService
	CrdRecheckSvc    service.CrdRecheckService
	// K8sCredSvc 集群凭证登记服务（3.4；HTTP 面端点未落地前经模块面暴露，
	// 供运维登记/脚本调用接线）。
	K8sCredSvc     service.K8sCredentialService
	AlertPublisher service.CertAlertPublisher // 供 ioc 装配 pause-timeout 处置通知

	// VerifyProbeIntervalMinutes 窗口调度周期（分钟）：装配期经
	// scheduler.ResolveVerifyProbeIntervalMinutes 从 AlertConfig.thresholds
	// 解析（AC-6）；DB 单文档，运行期改动需重启生效。
	VerifyProbeIntervalMinutes int

	// Web 面（InitWebServer 经 RegisterRoutes 挂载）
	Hdl          *web.CertHandler
	ReferenceHdl *web.ReferenceHandler
	LedgerHdl    *web.LedgerHandler
	DashboardHdl *web.DashboardHandler
	SettingsHdl  *web.SettingsHandler
	ChangeHdl    *web.ChangeHandler
}

// InitCertModule 装配 cert 功能域全量组件。
//
// deps 说明：
//   - db：Mongo（cert_* 集合族）
//   - logger：日志组件（nil 回退默认）
//   - accounts：云账号仓储（3.5 扫描账号源 + 5.7 执行凭证来源）
//   - instances：资产实例仓储（覆盖率分母独立盘点数据源，internal/asset）
//   - queue：taskx 任务队列（既有 cam 任务模块队列；nil 时子任务派发降级
//     为显式报错——批量执行 Execute 派发失败逐项隔离，不阻断装配）
//   - publisher：告警发布器（4.3 internal/alert 生产实现由 ioc 注入；
//     nil 回退日志发布）
//
// 启动失败语义（fail-fast，均返回 error 阻断 app boot）：
//   - EnsureIndexes 失败：uk_active_mutex 部分唯一索引承载在途互斥正确性
//     （5.1 体系），索引缺失即降级为纯应用层检查，不可静默启动；
//   - 信封加密装配失败：主密钥 env 与降级来源均不可用或配置非法（1.1 契约：
//     私钥信封加密为域核心安全前提，启动期 fail-fast；env 优先，未配置时
//     降级复用 security.encryption_key 派生 V1——用户决策"先复用现有配置"，
//     见 cryptoFallbackFromAppConfig）。
func InitCertModule(
	db *mongox.Mongo,
	logger *elog.Component,
	accounts accountrepo.CloudAccountRepository,
	instances assetrepo.InstanceRepository,
	queue *taskx.Queue,
	publisher service.CertAlertPublisher,
) (*Module, error) {
	if logger == nil {
		logger = elog.DefaultLogger
	}
	if publisher == nil {
		publisher = service.NewLoggingAlertPublisher()
	}

	// ---- 基础设施：索引 + 信封加密（fail-fast，见函数注释） ----
	if err := repository.EnsureIndexes(context.Background(), db); err != nil {
		return nil, fmt.Errorf("cert: ensure indexes: %w", err)
	}
	crypto, keySource, err := domain.NewEnvelopeCryptoWithFallback(cryptoFallbackFromAppConfig)
	if err != nil {
		return nil, fmt.Errorf("cert: envelope crypto: %w", err)
	}
	if keySource == domain.MasterKeySourceFallback {
		logger.Warn("cert: 未配置独立主密钥环境变量 EIAM_CERT_MASTER_KEY_V<n>，已降级复用 security.encryption_key 派生 V1 主密钥（与云账号 AK/SK 加密同源，隔离性弱于独立 env）；建议后续配置独立 env 并按 keyVersion 轮换迁移")
	}

	repos := repository.NewRepositories(db)

	// ---- 审计桥（7.2）：5.8/5.9/5.10/5.11 审计与报告存档端口统一经
	// internal/audit 落地（单集合仅追加；索引失败仅告警不阻断启动）----
	auditBridge := newChangeAuditBridge(db, logger)

	// ---- 执行通道（5.3 CloudAPI + 5.6 K8s；首期可部署双云注册） ----
	cloudChannel := deployer.NewCloudAPIChannel(
		repos.CloudMappings,
		deployer.NewLedgerMaterialSource(repos.Certificates, crypto),
		deployer.NewSnapshotOldRefSource(repos.ScanSnapshots, repos.CertReferences),
	)
	if err := cloudChannel.RegisterDeployer(
		string(domain.CloudAliyun),
		deployer.NewAliyunDeployer(aliyuncert.NewCertAdapter(logger), repos.CloudMappings),
		string(domain.ProductCDN), string(domain.ProductDCDN), string(domain.ProductWAF),
		string(domain.ProductALB), string(domain.ProductNLB),
	); err != nil {
		return nil, fmt.Errorf("cert: register aliyun deployer: %w", err)
	}
	if err := cloudChannel.RegisterDeployer(
		string(domain.CloudTencent),
		deployer.NewTencentDeployer(tencentcert.NewCertAdapter(logger), repos.CloudMappings),
		string(domain.ProductCDN), string(domain.ProductWAF), string(domain.ProductCLB),
	); err != nil {
		return nil, fmt.Errorf("cert: register tencent deployer: %w", err)
	}

	k8sFactory := k8s.NewFactory(repos.K8sCredentials, crypto)
	k8sChannel := deployer.NewK8sAPIChannel(
		deployer.K8sFactoryClients{Factory: k8sFactory},
		repos.CrdRegs,
		repos.CloudMappings,
		deployer.ManagementSignalConfig{}, // 零值 → 构造器回退默认三信号键集（应用 config 注入面留二期）
	)
	channels := []deployer.ExecutionChannel{cloudChannel, k8sChannel}

	// ---- 凭证来源（云 AK + K8s kubeconfig，解密仅内存） ----
	creds := service.NewAccountCredentialSource(accounts, repos.K8sCredentials, crypto)

	// ---- 服务装配（依赖序） ----
	importSvc := service.NewImportService(repos.Certificates, repos.BatchSessions, crypto)
	ledgerSvc := service.NewLedgerService(repos.Certificates, repos.CertReferences, repos.ScanSnapshots)
	scanSvc := service.NewReferenceScanService(
		repos.ScanSnapshots, repos.CertReferences, repos.CloudMappings,
		repos.CrdRegs, repos.AlertConfig,
		service.NewAssetRepositoryCounts(instances),
		[]service.CloudScanAdapter{
			service.NewAliyunScanAdapter(aliyuncert.NewCertAdapter(logger)),
			service.NewTencentScanAdapter(tencentcert.NewCertAdapter(logger)),
			service.NewHuaweiScanAdapter(huaweidiscover.NewCertDiscoveryAdapter(logger)),
			service.NewAwsScanAdapter(awsdiscover.NewCertDiscoveryAdapter(logger)),
			service.NewAzureScanAdapter(azurediscover.NewCertDiscoveryAdapter(logger)),
		},
		service.NewAccountScanSource(accounts),
		service.NewK8sScanGateway(k8sFactory),
		&publisherScanNotifier{publisher: publisher},
	)
	probeSvc := service.NewProbeService(repos.Certificates, repos.ProbeResults, repos.Exemptions, repos.AlertConfig, repos.ChangeOrders, nil)
	inspectionSvc := service.NewInspectionService(repos.Certificates, repos.AlertConfig, publisher)
	changeSvc := service.NewChangeService(repos.ChangeOrders, repos.ChangeItems, repos.Certificates,
		repos.AlertConfig, repos.ScanSnapshots, repos.CertReferences, k8sChannel)

	// 验证窗口（5.10）：recorder=auditBridge（UnmetDomains 存档 7.2 接线
	// internal/audit，5.11 报告聚合读侧同桥）；changes=ChangeService（终态迁移白名单）。
	verifyWindowSvc := service.NewVerifyWindowService(
		repos.ChangeOrders, repos.Certificates, repos.Exemptions, repos.AlertConfig,
		repos.ProbeResults, probeSvc, changeSvc, auditBridge, publisher,
	)

	// 子任务派发（5.7）：生产 TaskxItemDispatcher 挂既有任务队列；队列不可用
	// 时降级为显式报错（派发失败逐项隔离，装配不阻断）。
	var dispatch service.SubtaskDispatcher = unavailableDispatcher{}
	if queue != nil {
		dispatch = service.TaskxItemDispatcher{Queue: queue}
	}
	executeSvc := service.NewChangeExecuteService(
		repos.ChangeOrders, repos.ChangeItems, repos.Certificates, repos.AlertConfig,
		repos.ScanSnapshots, repos.CertReferences, channels, creds, dispatch,
		verifyWindowSvc, verifyWindowSvc,
		&publisherItemTimeoutNotifier{publisher: publisher}, // 7.2：恢复告警接线（ops 类通知）
		auditBridge, // 7.2：item_result 审计
	)
	if queue != nil {
		// 7.1 框架挂载：变更项子任务执行器注册到 internal/task 队列
		//（TaskTypeExecuteChangeItem；项级领取 CAS 保证框架重投递幂等）。
		queue.RegisterExecutor(service.NewChangeItemExecutor(executeSvc))
	}

	// 回滚（5.8）：targets=cloud 通道 GetCert 三判定；auditor=auditBridge
	//（回滚审计 7.2 接线 internal/audit）。
	rollbackSvc := service.NewChangeRollbackService(
		repos.ChangeOrders, repos.ChangeItems, repos.Certificates, repos.AlertConfig,
		repos.CloudMappings, channels, cloudChannel, creds, publisher, auditBridge,
	)
	// 孤儿清理与 CRD 复检（5.9）：recorder=auditBridge（报告存档 7.2 接线）。
	orphanSvc := service.NewOrphanCleanupService(
		repos.ChangeOrders, repos.ChangeItems, repos.Certificates, repos.CloudMappings,
		cloudChannel, creds, auditBridge, publisher,
	)
	crdRecheckSvc := service.NewCrdRecheckService(
		repos.ChangeOrders, repos.ChangeItems, repos.Certificates,
		repos.AlertConfig, k8sChannel, publisher,
	)
	settingsSvc := service.NewSettingsService(repos.AlertConfig, repos.Exemptions, publisher)
	crdRegSvc := service.NewCrdRegistrationService(repos.CrdRegs)
	k8sCredSvc := service.NewK8sCredentialService(repos.K8sCredentials, crypto, crdRegSvc, k8sFactory)

	// 巡检流水线（4.4）：运行记录默认内存实现（lastInspectionAt 进程内口径；
	// 持久化 InspectionRunStore 为 4.4 留缝——schema.sql 无巡检运行集合，
	// 新增集合属 7.2/后续存储接线，Job 侧零改动可替换）。
	runStore := scheduler.NewMemoryInspectionRunStore()
	inspectionJob := scheduler.NewInspectionJob(
		repos.Certificates, repos.Exemptions, inspectionSvc, probeSvc, publisher, runStore,
	)
	dashboardSvc := service.NewDashboardService(
		repos.Certificates, repos.CertReferences, repos.ScanSnapshots, repos.ProbeResults,
		repos.Exemptions, repos.AlertConfig, ledgerSvc, runStore,
	)
	// 变更查询（5.11）：unmet/orphan/audit=auditBridge（7.2 生产接线
	// internal/audit 单集合：按单审计流水 + 未达标/孤儿清理报告存档）。
	querySvc := service.NewChangeQueryService(
		repos.ChangeOrders, repos.ChangeItems, repos.ScanSnapshots, repos.ProbeResults,
		repos.AlertConfig, auditBridge, auditBridge, auditBridge,
	)

	return &Module{
		Repos:                      repos,
		ScanSvc:                    scanSvc,
		InspectionJob:              inspectionJob,
		VerifyWindowSvc:            verifyWindowSvc,
		ChangeSvc:                  changeSvc,
		ExecuteSvc:                 executeSvc,
		OrphanCleanupSvc:           orphanSvc,
		CrdRecheckSvc:              crdRecheckSvc,
		K8sCredSvc:                 k8sCredSvc,
		AlertPublisher:             publisher,
		VerifyProbeIntervalMinutes: scheduler.ResolveVerifyProbeIntervalMinutes(context.Background(), repos.AlertConfig),
		Hdl:                        web.NewCertHandler(importSvc),
		ReferenceHdl:               web.NewReferenceHandler(service.NewReferenceQueryService(repos.Certificates, repos.CertReferences, repos.ScanSnapshots, scanSvc)),
		LedgerHdl:                  web.NewLedgerHandler(ledgerSvc),
		DashboardHdl:               web.NewDashboardHandler(dashboardSvc),
		SettingsHdl:                web.NewSettingsHandler(settingsSvc, crdRegSvc),
		ChangeHdl:                  web.NewChangeHandler(querySvc, changeSvc, executeSvc, rollbackSvc, auditBridge),
	}, nil
}

// RegisterRoutes 在 server 上挂载证书域路由（/api/v1/certs 前缀；鉴权由全局
// 中间件链承接，见 web/router.go）。
func (m *Module) RegisterRoutes(server *gin.Engine) {
	if server == nil || m == nil {
		return
	}
	web.RegisterRoutes(server, m.Hdl, m.ReferenceHdl, m.LedgerHdl, m.DashboardHdl, m.SettingsHdl, m.ChangeHdl)
}

// ---------------------------------------------------------------------
// 装配期小适配器
// ---------------------------------------------------------------------

// publisherScanNotifier scan-timeout 恢复告警适配（3.5 ScanAlertNotifier →
// 4.3 CertAlertPublisher，ops 运维处置类；通知失败不阻塞恢复流程——
// 3.5 端口契约由调用侧吸收）。
type publisherScanNotifier struct {
	publisher service.CertAlertPublisher
}

// NotifyScanTimedOut 发布扫描超时恢复处置通知（快照已转 failed、防重锁已释放）。
func (n *publisherScanNotifier) NotifyScanTimedOut(ctx context.Context, snapshotID string, startedAt, recoveredAt time.Time) error {
	if n.publisher == nil {
		return nil
	}
	return n.publisher.PublishAlert(ctx, service.CertAlertEvent{
		Category: service.AlertCategoryOps,
		Title:    "证书引用扫描超时自动恢复",
		Detail: fmt.Sprintf("scan snapshot %s (started %s) exceeded scanTimeoutHours and was marked failed at %s; scan lock released, rescan allowed",
			snapshotID, startedAt.Format(time.RFC3339), recoveredAt.Format(time.RFC3339)),
		At: recoveredAt,
	})
}

// unavailableDispatcher 任务队列不可用时的降级派发器：显式报错（Execute 逐项
// 隔离派发失败，项保持 pending 可重入；装配不因队列缺失阻断 boot）。
type unavailableDispatcher struct{}

// DispatchItem 恒返回队列不可用错误。
func (unavailableDispatcher) DispatchItem(context.Context, string, string) error {
	return fmt.Errorf("cert: task queue unavailable; change item subtask dispatch disabled")
}

// publisherItemTimeoutNotifier executing-timeout 恢复通知适配（7.2 接线：
// 5.7 ExecuteAlertNotifier → 4.3 CertAlertPublisher，ops 运维处置类；
// 通知失败不阻塞恢复流程——5.7 端口契约由调用侧吸收）。
type publisherItemTimeoutNotifier struct {
	publisher service.CertAlertPublisher
}

// NotifyItemTimedOut 发布心跳超时项恢复通知（附订单/项定位与心跳时点）。
func (n *publisherItemTimeoutNotifier) NotifyItemTimedOut(ctx context.Context, orderID, itemID string, heartbeatAt, recoveredAt time.Time) error {
	if n.publisher == nil {
		return nil
	}
	return n.publisher.PublishAlert(ctx, service.CertAlertEvent{
		Category: service.AlertCategoryOps,
		Title:    "变更项执行超时自动恢复",
		Detail: fmt.Sprintf("change item %s of order %s (heartbeat %s) timed out and was marked failed at %s; order status recomputed from remaining items",
			itemID, orderID, heartbeatAt.Format(time.RFC3339), recoveredAt.Format(time.RFC3339)),
		At: recoveredAt,
	})
}

// cryptoFallbackFromAppConfig 主密钥 env 缺失时的降级来源：
// 复用应用 config security.encryption_key（云账号 AK/SK 加密已在用的
// 密钥材料，test.yaml/prod.yaml 已有），由 NewEnvelopeCryptoWithFallback
// 经 SHA-256 派生为 32 字节 V1 主密钥。键缺失/为空返回 ok=false。
func cryptoFallbackFromAppConfig() ([]byte, bool) {
	var sec struct {
		EncryptionKey string `mapstructure:"encryption_key"`
	}
	if err := viper.UnmarshalKey("security", &sec); err != nil || sec.EncryptionKey == "" {
		return nil, false
	}
	return []byte(sec.EncryptionKey), true
}

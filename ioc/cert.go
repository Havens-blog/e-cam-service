package ioc

import (
	"context"

	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	accountdao "github.com/Havens-blog/e-cam-service/internal/account/repository/dao"
	alertdao "github.com/Havens-blog/e-cam-service/internal/alert/repository/dao"
	assetrepo "github.com/Havens-blog/e-cam-service/internal/asset/repository"
	assetdao "github.com/Havens-blog/e-cam-service/internal/asset/repository/dao"
	auditdao "github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
	auditservice "github.com/Havens-blog/e-cam-service/internal/audit/service"
	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/Havens-blog/e-cam-service/internal/cert"
	"github.com/Havens-blog/e-cam-service/internal/cert/alertpub"
	"github.com/Havens-blog/e-cam-service/internal/cert/repository"
	"github.com/Havens-blog/e-cam-service/internal/cert/scheduler"
	certservice "github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-common-go/mongox"
	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/gotomicro/ego/core/elog"
	"github.com/gotomicro/ego/task/ecron"
)

// InitCertModule 初始化证书管理功能域模块（任务 7.1，风格对齐 InitAlertModule）。
//
// 依赖来源（既有装配复用，不新增 provider）：
//   - db：与 cam/alert 同一 Mongo；
//   - camModule：云账号/资产/任务队列经 cam 模块既有仓储与任务队列取得
//     （taskx 队列归 cam TaskModule 所有——ChangeItemExecutor 复用同队列，
//     与 RegisterBillingExecutor 同机制）。
//
// CertAlertPublisher 生产装配（cert 自持：发布器实现在 internal/cert/alertpub，
// 对 alert 零 import；本组合根构造后注入 cert 模块）：webhook+email 双通道，
// SMTP 凭据经 loadCertSMTPConfig 从应用 config alert.cert_smtp 读取、并由
// 本组合根的 adapter 适配 alert 通用基建到 cert 端口（见 ioc/cert_alert.go）
// （未配置仅停用邮件通道并告警，webhook 不受影响——4.3 Hard）。
func InitCertModule(db *mongox.Mongo, camModule *cam.Module) (*cert.Module, error) {
	logger := elog.DefaultLogger
	accounts := accountrepo.NewCloudAccountRepository(accountdao.NewCloudAccountDAO(db))
	instances := assetrepo.NewInstanceRepository(assetdao.NewInstanceDAO(db))

	var queue *taskx.Queue
	if camModule != nil && camModule.TaskModule != nil && camModule.TaskModule.Queue != nil {
		queue = camModule.TaskModule.Queue
	} else {
		logger.Warn("cert: cam 任务队列不可用，变更项子任务派发降级为显式报错（不阻断启动）")
	}

	// 证书告警发布器（cert 自持；cert/alertpub 对 alert 零 import）：
	// 通用 SMTP 邮件发送与投递记录持久化由本组合根用 alert 既有基建适配到
	// cert 端口（见 ioc/cert_alert.go）。SMTP 未配置 → email sink 传 nil，
	// 发布器据此停用邮件通道并告警，webhook 不受影响（4.3 Hard）。
	smtp := loadCertSMTPConfig()
	var emailSink alertpub.EmailSink
	if smtp.Host != "" {
		emailSink = certEmailSinkAdapter{cfg: smtp}
	}
	publisher := alertpub.NewCertAlertPublisher(
		repository.NewAlertConfigRepository(db),
		certDeliveryRecorderAdapter{dao: alertdao.NewAlertDAO(db)},
		emailSink,
		logger,
	)
	// DNS 记录只读端口（cam/dns 模块暴露）：未装配时 dnsSource=nil，cert probe
	// 回退台账 SAN 路径；装配后 ProbeAllTenantDNS 以 DNS 记录为源覆盖通配符子域名。
	// adapter 将 cam/dns.RecordReadPort 的 ProbeTarget/LinkedResource 翻译为 cert
	// 自有投影，使 cert 不再 import internal/cam/dns（见 ioc/cert_dns.go）。
	var dnsSource certservice.DNSRecordSource
	if camModule != nil && camModule.DNSRecordReadPort != nil {
		dnsSource = certDNSRecordSource{port: camModule.DNSRecordReadPort}
	}

	// 变更单审计服务（7.2）：DAO 构造 + 索引初始化属持久化装配，置于组合根；
	// 索引失败仅告警不阻断启动（缺索引仅影响去重键唯一性约束，流水写入/查询不受阻）。
	auditDAO := auditdao.NewChangeOrderAuditDAO(db)
	if err := auditDAO.InitIndexes(context.Background()); err != nil {
		logger.Error("cert: 变更单审计索引初始化失败（仅告警，不阻断启动）", elog.FieldErr(err))
	}
	audits := auditservice.NewChangeOrderAuditService(auditDAO, logger)

	return cert.InitCertModule(db, logger, accounts, assetInstanceCounter{repo: instances}, queue, publisher, dnsSource, certAuditStore{svc: audits})
}

// initCertJobs 构建 cert 域 10 类定时任务（9 个调度点）的 ecron 组件（任务 7.1；
// cert:cert-import 由 cert-volcano-import-sync 任务 4 接入）。
//
// 窗口周期（AC-6）：verifyProbeIntervalMinutes 于模块装配期从
// AlertConfig.thresholds 解析（DB 单文档，运行期改动需重启生效）；
// 其余阈值由各服务函数运行期自行读取。
// 门控与 cam 任务一致：cronjob.enabled（InitJobs 统一判定）。
func initCertJobs(certModule *cert.Module, logger *elog.Component) []*ecron.Component {
	if certModule == nil {
		return nil
	}
	jobs := &scheduler.CertJobs{
		Scan:       certModule.ScanSvc,
		Inspection: certModule.InspectionJob,
		Windows:    certModule.VerifyWindowSvc,
		Changes:    certModule.ChangeSvc,
		Execute:    certModule.ExecuteSvc,
		Orphan:     certModule.OrphanCleanupSvc,
		Recheck:    certModule.CrdRecheckSvc,
		Sync:       certModule.CertSyncSvc, // cert:cert-import 多云增量同步（volcano-import 任务 3）
		Publisher:  certModule.AlertPublisher,
	}
	specs := jobs.JobSpecs(certModule.VerifyProbeIntervalMinutes)
	components := make([]*ecron.Component, 0, len(specs))
	for _, spec := range specs {
		components = append(components, ecron.DefaultContainer().Build(
			ecron.WithJob(ecron.FuncJob(spec.Run)),
			ecron.WithSpec(spec.Spec),
		))
	}
	logger.Info("cert 定时任务注册完成", elog.Int("job_count", len(components)))
	return components
}

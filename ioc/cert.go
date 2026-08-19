package ioc

import (
	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	accountdao "github.com/Havens-blog/e-cam-service/internal/account/repository/dao"
	alertdao "github.com/Havens-blog/e-cam-service/internal/alert/repository/dao"
	alertservice "github.com/Havens-blog/e-cam-service/internal/alert/service"
	assetrepo "github.com/Havens-blog/e-cam-service/internal/asset/repository"
	assetdao "github.com/Havens-blog/e-cam-service/internal/asset/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/Havens-blog/e-cam-service/internal/cert"
	"github.com/Havens-blog/e-cam-service/internal/cert/repository"
	"github.com/Havens-blog/e-cam-service/internal/cert/scheduler"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
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
// CertAlertPublisher 生产装配（4.3 依赖方向 alert→cert，cert 永不反向 import：
// 发布器由本组合根构造后注入 cert 模块）：webhook+email 双通道，
// SMTP 凭据经 LoadCertSMTPConfig 从应用 config alert.cert_smtp 读取
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

	publisher := alertservice.NewCertAlertPublisher(
		repository.NewAlertConfigRepository(db),
		alertdao.NewAlertDAO(db),
		alertservice.LoadCertSMTPConfig(),
		logger,
	)
	return cert.InitCertModule(db, logger, accounts, instances, queue, publisher)
}

// initCertJobs 构建 cert 域 9 类定时任务的 ecron 组件（任务 7.1）。
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

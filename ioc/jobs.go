package ioc

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/Havens-blog/e-cam-service/internal/cert"
	"github.com/gotomicro/ego/core/elog"
	"github.com/gotomicro/ego/task/ecron"
	"github.com/spf13/viper"
)

// InitJobs 构建全部定时任务（cam 域既有任务 + cert 域 9 类任务，7.1 扩签名）。
// 既有 cam 任务条目零改动（AC-4：不改动既有 task 注册项）；cert 任务经
// initCertJobs 独立追加（新增项独立）。
func InitJobs(camModule *cam.Module, certModule *cert.Module) []*ecron.Component {
	type Config struct {
		Enabled bool `mapstructure:"enabled"`
	}
	var cfg Config

	err := viper.UnmarshalKey("cronjob", &cfg)
	if err != nil {
		panic(err)
	}

	// 如果cronjob未启用，返回空列表
	if !cfg.Enabled {
		return []*ecron.Component{}
	}

	logger := elog.DefaultLogger
	var jobs []*ecron.Component

	// 账单采集：每 6 小时执行一次 (0 */6 * * *)
	if camModule.CostCollectorSvc != nil {
		collectorSvc := camModule.CostCollectorSvc
		jobs = append(jobs, ecron.DefaultContainer().Build(
			ecron.WithJob(ecron.FuncJob(func(ctx context.Context) error {
				logger.Info("开始定时账单采集")
				return collectorSvc.StartScheduledCollection(ctx)
			})),
			ecron.WithSpec("0 */6 * * *"),
		))
	}

	// 预算检查：每日 8:00 执行 (0 8 * * *)
	// 按租户枚举逐个执行：从预算集合 distinct tenant_id（方案A，不经 eiam），
	// 无预算规则的租户无检查目标自然不入轮。单租户失败不中断（errors.Join 聚合）。
	if camModule.CostBudgetSvc != nil {
		budgetSvc := camModule.CostBudgetSvc
		jobs = append(jobs, ecron.DefaultContainer().Build(
			ecron.WithJob(ecron.FuncJob(func(ctx context.Context) error {
				logger.Info("开始每日预算检查（按租户枚举逐个执行）")
				return budgetSvc.RunForAllTenants(ctx)
			})),
			ecron.WithSpec("0 8 * * *"),
		))
	}

	// 异常检测：每日 6:00 执行 (0 6 * * *)
	// 按租户枚举逐个执行：从账单集合 distinct tenant_id（方案A，不经 eiam），
	// 无账单的租户无可分析数据自然不入轮。检测日期（昨日）由服务内部计算。
	if camModule.CostAnomalySvc != nil {
		anomalySvc := camModule.CostAnomalySvc
		jobs = append(jobs, ecron.DefaultContainer().Build(
			ecron.WithJob(ecron.FuncJob(func(ctx context.Context) error {
				logger.Info("开始每日异常检测（按租户枚举逐个执行）")
				return anomalySvc.RunForAllTenants(ctx)
			})),
			ecron.WithSpec("0 6 * * *"),
		))
	}

	// 优化建议生成：每日 7:00 执行 (0 7 * * *)
	// 按租户枚举逐个执行：从账单集合 distinct tenant_id（方案A，不经 eiam），
	// 无账单的租户无可分析数据自然不入轮。
	if camModule.CostOptimizerSvc != nil {
		optimizerSvc := camModule.CostOptimizerSvc
		jobs = append(jobs, ecron.DefaultContainer().Build(
			ecron.WithJob(ecron.FuncJob(func(ctx context.Context) error {
				logger.Info("开始每日优化建议生成（按租户枚举逐个执行）")
				return optimizerSvc.RunForAllTenants(ctx)
			})),
			ecron.WithSpec("0 7 * * *"),
		))
	}

	// cert 域 9 类定时任务（7.1 新增项，独立于既有 cam 条目注册）
	jobs = append(jobs, initCertJobs(certModule, logger)...)

	return jobs
}

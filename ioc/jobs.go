package ioc

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/gotomicro/ego/core/elog"
	"github.com/gotomicro/ego/task/ecron"
	"github.com/spf13/viper"
)

func InitJobs(camModule *cam.Module) []*ecron.Component {
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
	if camModule.CostBudgetSvc != nil {
		budgetSvc := camModule.CostBudgetSvc
		jobs = append(jobs, ecron.DefaultContainer().Build(
			ecron.WithJob(ecron.FuncJob(func(ctx context.Context) error {
				logger.Info("开始每日预算检查")
				return budgetSvc.CheckBudgets(ctx, "")
			})),
			ecron.WithSpec("0 8 * * *"),
		))
	}

	// 异常检测：每日 6:00 执行 (0 6 * * *)
	if camModule.CostAnomalySvc != nil {
		anomalySvc := camModule.CostAnomalySvc
		jobs = append(jobs, ecron.DefaultContainer().Build(
			ecron.WithJob(ecron.FuncJob(func(ctx context.Context) error {
				logger.Info("开始每日异常检测")
				yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
				return anomalySvc.DetectAnomalies(ctx, "", yesterday)
			})),
			ecron.WithSpec("0 6 * * *"),
		))
	}

	// 优化建议生成：每日 7:00 执行 (0 7 * * *)
	if camModule.CostOptimizerSvc != nil {
		optimizerSvc := camModule.CostOptimizerSvc
		jobs = append(jobs, ecron.DefaultContainer().Build(
			ecron.WithJob(ecron.FuncJob(func(ctx context.Context) error {
				logger.Info("开始每日优化建议生成")
				return optimizerSvc.GenerateRecommendations(ctx, "")
			})),
			ecron.WithSpec("0 7 * * *"),
		))
	}

	return jobs
}

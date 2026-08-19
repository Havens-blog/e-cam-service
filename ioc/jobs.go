package ioc

import (
	"context"
	"time"

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
	if camModule.CostBudgetSvc != nil {
		budgetSvc := camModule.CostBudgetSvc
		jobs = append(jobs, ecron.DefaultContainer().Build(
			ecron.WithJob(ecron.FuncJob(func(ctx context.Context) error {
				// 传 0 表示「未选定租户」，按设计 §4.3 它不是「全部租户」通配。
				// 迁移前这里传 "" —— 但 budgetDAO.ListActive 的 tenant_id 过滤本来
				// 就是无条件的，故 "" 匹配不到任何预算，该定时任务一直是空转。
				//
				// 用 warn 而非 info：否则日志只有「开始每日预算检查」+ 返回 nil，
				// 运维无法区分「扫过了但没数据」与「结构上无法扫描」。
				//
				// TODO(tenant-unification): 改为遍历 eiam 返回的租户列表逐个执行。
				logger.Warn("每日预算检查未实际执行：租户枚举尚不支持，本次不会检查任何预算",
					elog.String("todo", "tenant-unification: 遍历 eiam 租户列表逐个执行"))
				return budgetSvc.CheckBudgets(ctx, 0)
			})),
			ecron.WithSpec("0 8 * * *"),
		))
	}

	// 异常检测：每日 6:00 执行 (0 6 * * *)
	if camModule.CostAnomalySvc != nil {
		anomalySvc := camModule.CostAnomalySvc
		jobs = append(jobs, ecron.DefaultContainer().Build(
			ecron.WithJob(ecron.FuncJob(func(ctx context.Context) error {
				// 传 0 表示「未选定租户」，不是「全部租户」通配。
				// 迁移前传 "" 会命中 DAO 里「空租户即不加过滤」的分支，从而跨所有
				// 租户聚合账单并据此写入异常记录 —— 正是本次要消除的越界行为。
				// 现在租户过滤恒定生效，本任务将查不到数据而空转，直到支持租户枚举。
				//
				// 用 warn 而非 info，理由同预算检查：让「结构上无法扫描」在日志里可见。
				//
				// TODO(tenant-unification): 改为遍历 eiam 返回的租户列表逐个执行。
				logger.Warn("每日异常检测未实际执行：租户枚举尚不支持，本次不会检测任何租户",
					elog.String("todo", "tenant-unification: 遍历 eiam 租户列表逐个执行"))
				yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
				return anomalySvc.DetectAnomalies(ctx, 0, yesterday)
			})),
			ecron.WithSpec("0 6 * * *"),
		))
	}

	// 优化建议生成：每日 7:00 执行 (0 7 * * *)
	if camModule.CostOptimizerSvc != nil {
		optimizerSvc := camModule.CostOptimizerSvc
		jobs = append(jobs, ecron.DefaultContainer().Build(
			ecron.WithJob(ecron.FuncJob(func(ctx context.Context) error {
				// 传 0 表示「未选定租户」，不是「全部租户」通配。
				// 与异常检测同理：迁移前传 "" 会跨全部租户聚合账单来生成优化建议。
				// 现在租户过滤恒定生效，本任务将空转，直到支持租户枚举。
				//
				// 用 warn 而非 info，理由同上。
				//
				// TODO(tenant-unification): 改为遍历 eiam 返回的租户列表逐个执行。
				logger.Warn("每日优化建议生成未实际执行：租户枚举尚不支持，本次不会产生任何建议",
					elog.String("todo", "tenant-unification: 遍历 eiam 租户列表逐个执行"))
				return optimizerSvc.GenerateRecommendations(ctx, 0)
			})),
			ecron.WithSpec("0 7 * * *"),
		))
	}

	// cert 域 9 类定时任务（7.1 新增项，独立于既有 cam 条目注册）
	jobs = append(jobs, initCertJobs(certModule, logger)...)

	return jobs
}

// Package executor OSS 自我健康监控(任务 6)。
//
// 文件：internal/cam/task/executor/oss_health_monitor.go
//
// 作用：挂在每日 OSS 指标采集完成钩子上，对必达厂商(aliyun/huawei/aws，
// 与 T1 报告必达分组一致)检查近 N 天(默认 3 天)是否有成功写库行——
// 以 ecam_oss_metric 中该厂商的行存在为「成功采集」证据；连续 N 天全零
// 且该厂商实盘存在 ≥1 个 OSS bucket 时，经共用告警通道升级告警。
//
// 蓝本：nas_health_monitor.go(NAS 自我健康监控)整体平移，判定口径一致;
// Hard Rule:
//   - 必须带「该厂商实盘存在 ≥1 个 OSS bucket」前置——无 bucket 的厂商零成功
//     属预期，告警会造成每 3 天稳定误报，告警疲劳反而掩盖真实失效；
//   - 仅全量运行判定，手动单账号/单厂商运行只反映局部状态，不判定(防误报)；
//   - 告警通道与日闸故障/NAS 健康监控共用同一告警桥(cam.schedulerGateAlerter，
//     同一定义勿重复造)，生产装配见 cam/wire.go。
//
// spec：docs/proposals/oss-ops-insight/proposal.md「Non-Functional Requirements」
// 可观测性条目(必达厂商近 3 天零成功写库 → 健康告警)。
package executor

import (
	"context"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/gotomicro/ego/core/elog"
)

// OSSHealthAlerter OSS 自我健康监控告警通道。
// Hard Rule:与日闸故障/NAS 健康监控共用同一告警桥(同一定义勿重复造)。
type OSSHealthAlerter interface {
	// AlertOSSZeroSuccess 必达厂商连续 windowDays 天零成功采集、且实盘存在
	// bucketCount(≥1)个 OSS bucket 时触发升级告警(前置由执行器核验)。
	AlertOSSZeroSuccess(ctx context.Context, provider string, windowDays int, bucketCount int64)
}

const (
	// ossHealthWindowDays 自我健康监控回看窗口:连续 N 天零成功 → 升级告警
	// (spec 默认 3 天,与 NAS 同口径;观测窗口内瞬时故障容忍,连续 3 天全零
	// 才升级,避免单日厂商 API 宕机即误报)
	ossHealthWindowDays = nasHealthWindowDays
)

// ossMandatoryProviders 必达厂商清单(与 T1 报告一致:必达 = aliyun/huawei/aws;
// tencent/volcengine 维持尽力而为,零成功属预期,不参与本监控)。
var ossMandatoryProviders = nasMandatoryProviders

// SetOSSHealthAlerter 注入自我健康监控告警桥(nil 关闭监控,仅最小装配用;
// 生产装配:cam/wire.go 与日闸/NAS 健康监控共用同一 schedulerGateAlerter 实例)
func (e *SyncOSSMetricsExecutor) SetOSSHealthAlerter(a OSSHealthAlerter) {
	e.healthAlerter = a
}

// checkMandatoryProviderHealth 每日采集完成钩子:检查各必达厂商近
// ossHealthWindowDays 天是否有成功写库行,连续全零且实盘存在 OSS bucket 的
// 厂商触发升级告警。返回本次升级告警的厂商清单(入任务 Result,运营可查)。
//
// 仅全量运行(AccountID/Provider 参数均未指定)判定——手动单账号/单厂商
// 运行只反映局部状态,以偏概全会造成误报。
func (e *SyncOSSMetricsExecutor) checkMandatoryProviderHealth(ctx context.Context, params syncOSSMetricsParams) []string {
	alerts := make([]string, 0)
	if params.AccountID != 0 || params.Provider != "" {
		return alerts // 手动局部运行不判定
	}
	if e.healthAlerter == nil {
		return alerts // 未装配告警桥(最小装配):安全跳过
	}

	// 近 N 天窗口含今日([今日-N+1, 今日]):每日日闸认领后必跑全量采集,
	// 采集成功则今日行已落库,窗口内行存在即「成功采集」证据
	since := time.Now().In(nasMetricsCSTZone).
		AddDate(0, 0, -(ossHealthWindowDays - 1)).Format("2006-01-02")
	counts, err := e.metricDAO.CountMetricsByProviders(ctx, ossMandatoryProviders, since)
	if err != nil {
		// 健康监控自身故障只记日志,不反噬采集主链路(任务结果已定)
		e.logger.Error("OSS 自我健康监控:统计必达厂商成功行数失败",
			elog.String("since", since),
			elog.FieldErr(err))
		return alerts
	}

	for _, p := range ossMandatoryProviders {
		if counts[p] > 0 {
			continue // 窗口内有成功写库行:健康
		}
		bucketCount, err := e.countOSSBuckets(ctx, p)
		if err != nil {
			e.logger.Error("OSS 自我健康监控:统计厂商 OSS bucket 数失败",
				elog.String("provider", p),
				elog.FieldErr(err))
			continue
		}
		if bucketCount == 0 {
			// Hard Rule:无 OSS bucket 的厂商不触发零成功告警(防误报)
			continue
		}
		e.logger.Error("OSS 自我健康监控:必达厂商连续零成功采集,升级告警",
			elog.String("provider", p),
			elog.Int("window_days", ossHealthWindowDays),
			elog.Int64("oss_buckets", bucketCount))
		e.healthAlerter.AlertOSSZeroSuccess(ctx, p, ossHealthWindowDays, bucketCount)
		alerts = append(alerts, p)
	}
	return alerts
}

// countOSSBuckets ecam_instance 中该厂商的 OSS bucket 数(全租户口径;
// 只要 total ≥1 即满足告警前置,故 Limit 取 1 只取 total)。
func (e *SyncOSSMetricsExecutor) countOSSBuckets(ctx context.Context, provider string) (int64, error) {
	_, total, err := e.instanceRepo.Search(ctx, camdomain.SearchFilter{
		AssetTypes: []string{"oss"},
		Provider:   provider,
		Limit:      1,
	})
	return total, err
}

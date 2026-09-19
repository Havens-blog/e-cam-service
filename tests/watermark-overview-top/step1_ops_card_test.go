// @feature nas-ops-insight @api-functional
//
// Contract step-1-ops-card: 查看运营卡水位聚合 — fs-level dedup, shared-fs
// single counting via the representative row, average denominator skips
// no-data and capacity=0 rows, and the collect-failed api signal stays
// distinguishable from zero placeholder.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package watermark_overview_top

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// newJourneyHarness 构建带一个可指标厂商的世界。
func newJourneyHarness() (*nastest.Harness, *nastest.Provider) {
	h := nastest.NewHarness()
	return h, h.NewProvider(true)
}

// seedOpsCardWorld 两账号:共享 fs-shared(账号 1/2,各留一行)+ 独占 fs-solo;
// 另有 zero_exception 行与无数据 fs 验证均值分母。
func seedOpsCardWorld(t *testing.T) (*nastest.Harness, *nastest.Provider) {
	t.Helper()
	h, provider := newJourneyHarness()
	acc1 := h.SeedAccount(1, provider)
	acc2 := h.SeedAccount(2, provider)
	h.SeedMetric(t, acc1.ID, provider.Name,
		nastest.Metric("fs-shared", "共享盘", nastest.Today(), 100, 50))
	h.SeedMetric(t, acc2.ID, provider.Name,
		nastest.Metric("fs-shared", "共享盘", nastest.Today(), 300, 90))
	h.SeedMetric(t, acc1.ID, provider.Name,
		nastest.Metric("fs-solo", "独占盘", nastest.DayOffset(-1), 400, 300))
	h.SeedMetric(t, acc1.ID, provider.Name,
		nastest.Metric("fs-solo", "独占盘", nastest.Today(), 400, 380))
	h.SeedMetric(t, acc1.ID, provider.Name,
		nastest.Metric("fs-zero", "零容量盘", nastest.Today(), 0, 0))
	return h, provider
}

// Outcome success: 聚合按 fs_id 去重后计数;多账号并存时按「日期 desc 再容量
// desc」取第一行作为该物理 fs 的容量口径;不做跨账号容量求和或平均。
func TestStep1_OpsCard_DedupAndRepresentativeRow(t *testing.T) {
	h, _ := seedOpsCardWorld(t)
	query := h.NewQueryService()

	top, err := query.GetTop(context.Background(), h.TenantID, 0, 30,
		service.NASSortCapacity, 10, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 3, top.Total, "共享 fs 去重:fs-shared 只计一次(fs-shared/fs-solo/fs-zero)")

	var shared *service.NASTopItem
	for i := range top.Items {
		if top.Items[i].FsID == "fs-shared" {
			shared = &top.Items[i]
		}
	}
	require.NotNil(t, shared)
	require.NotNil(t, shared.Latest.Capacity)
	require.Equal(t, float64(300), *shared.Latest.Capacity,
		"「日期 desc 再容量 desc」第一行 300;不求和(400)不平均(200)")
}

// Outcome shared-fs-dedup: 同一 fs_id 有 3 个账号各留一行指标时,该物理 fs
// 在运营卡与 Top 中只计一次,不因共享被双计。
func TestStep1_OpsCard_SharedFsDedup(t *testing.T) {
	h, provider := newJourneyHarness()
	for id := int64(1); id <= 3; id++ {
		acc := h.SeedAccount(id, provider)
		h.SeedMetric(t, acc.ID, provider.Name,
			nastest.Metric("fs-tri", "三方共享盘", nastest.Today(), float64(100*id), float64(30*id)))
	}

	top, err := h.NewQueryService().GetTop(context.Background(), h.TenantID, 0, 30,
		service.NASSortCapacity, 10, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 1, top.Total, "三账号共享 fs 只计一次")
}

// Outcome average-denominator-skip: 无数据实例跳过不参与分母;capacity=0 行
// 也不参与均值、作为异常单独可见 — 均值不因 0 值行被拉低。
func TestStep1_OpsCard_AverageDenominatorSkip(t *testing.T) {
	h, _ := seedOpsCardWorld(t)
	query := h.NewQueryService()

	// fs-solo:今日 380/400,昨日 300/400 → 均值分母只含正常两日
	trend, err := query.GetFsMetrics(context.Background(), h.TenantID, 1, "fs-solo", 7)
	require.NoError(t, err)
	require.NotNil(t, trend.Average)
	require.NotNil(t, trend.Average.Utilization)
	require.InDelta(t, (0.95+0.75)/2, *trend.Average.Utilization, 1e-9,
		"均值只由正常非零行构成,缺失日/零容量行不进分母")

	// zero_exception 行单独可见(data_status 映射),utilization null
	zeroTrend, err := query.GetFsMetrics(context.Background(), h.TenantID, 1, "fs-zero", 7)
	require.NoError(t, err)
	zeroToday := zeroTrend.Days[len(zeroTrend.Days)-1]
	require.Equal(t, service.NASDataStatusZeroException, zeroToday.DataStatus)
	require.Nil(t, zeroToday.Utilization)
}

// Outcome collect-failure-warning-priority: 最近一次采集任务 Result 失败计数
// 大于 0 时,api 面以非空 failures 表达「采集失败→警示」信号(优先于
// 「无数据→0 占位」);display 分支为前端逻辑,见 doc.go scope note。
func TestStep1_OpsCard_CollectFailureWarningPriority(t *testing.T) {
	h, okProvider := newJourneyHarness()
	badProvider := h.NewProvider(true)
	h.SeedAccount(1, okProvider)
	h.SeedAccount(2, badProvider)
	h.SeedInstance(1, okProvider, "fs-ok", "健康盘", "cn-hangzhou")
	h.SeedInstance(2, badProvider, "fs-bad", "故障盘", "cn-shanghai")
	okProvider.Querier.SetMetrics(nastest.Metric("fs-ok", "健康盘", nastest.Today(), 100, 10))
	badProvider.Querier.Fail(errors.New("injected: vendor collect failed"))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	failures := nastest.ResultFailures(t, task)
	require.NotEmpty(t, failures, "采集失败的警示信号(failures 非空)与 0 占位可分辨")
}

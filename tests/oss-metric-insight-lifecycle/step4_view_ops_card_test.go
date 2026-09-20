// @feature oss-ops-insight @api-functional
//
// Contract step-4-view-ops-card: 用户在 OSS 列表页查看运营卡 — every number
// the ops card derives from ( Top aggregation, bucket trend ) comes from the
// ecam_oss_metric table; the asset snapshot value never leaks into any OSS
// read response. unauthorized-401 is exempt ( see doc.go ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package oss_metric_insight_lifecycle

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome ops-card-from-metric-table: 运营卡数值全部来自 ecam_oss_metric
// 聚合(Top + 趋势),多日数据齐备时正常展示最新值与近 N 天均值。
func TestStep4_OpsCardFromMetricTable(t *testing.T) {
	h, provider := newJourneyHarness()
	seedBucketAccount(t, h, provider, 1, "web-assets", "log-archive")
	// 含今日在内的多个自然日(运营卡 7 天增速的底座数据)
	provider.Querier.SetMetrics(
		osstest.Metric("web-assets", osstest.DayOffset(-1), 590, 4100),
		osstest.Metric("web-assets", osstest.Today(), 600, 4200),
		osstest.Metric("log-archive", osstest.DayOffset(-1), 1090, 110),
		osstest.Metric("log-archive", osstest.Today(), 1100, 120),
	)
	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	top, err := h.NewQueryService().GetTop(context.Background(), 1, 0, 7, service.OSSSortStorageSize, 10, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 2, top.Total)
	for _, item := range top.Items {
		// 运营卡「最新值」来自指标表最新一天
		require.NotNil(t, item.Latest.StorageSize)
		// 近 7 天均值来自指标表多日聚合
		require.NotNil(t, item.Average.StorageSize)
		switch item.BucketName {
		case "web-assets":
			require.Equal(t, float64(600), *item.Latest.StorageSize)
			require.InDelta(t, 595, *item.Average.StorageSize, 1e-9)
		case "log-archive":
			require.Equal(t, float64(1100), *item.Latest.StorageSize)
			require.InDelta(t, 1095, *item.Average.StorageSize, 1e-9)
		}
	}
}

// Outcome unauthorized: 豁免(见 doc.go)——认证由全局鉴权中间件承载,
// OSS 表面测试路由不含认证层,无法在表面内复现 401。
func TestStep4_OpsCard_UnauthorizedExempt(t *testing.T) {
	t.Log("exempt: 401 enforced by global auth middleware outside the OSS surface")
}

// Outcome snapshot-metric-mismatch: 资产表快照与指标表不一致(资产同步滞后)
// 时,OSS 读取响应一律显示指标表数据;资产表快照数值不出现在任何响应字段中
// (资产表仅作 bucket 枚举与元数据)。
func TestStep4_SnapshotMetricMismatch_IndicatorTableWins(t *testing.T) {
	h, provider := newJourneyHarness()
	// 资产表快照 storage_size=-1(见 BucketRepo.SeedBucket),与指标值 600 相差悬殊
	seedBucketAccount(t, h, provider, 1, "lagged-bucket")
	seedMetric(t, h, 1, provider.Name, osstest.Metric("lagged-bucket", osstest.Today(), 600, 42))

	// 趋势读取:数值来自指标表
	resp, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, "lagged-bucket", 7)
	require.NoError(t, err)
	today := resp.Days[len(resp.Days)-1]
	require.NotNil(t, today.StorageSize)
	require.Equal(t, float64(600), *today.StorageSize, "趋势容量必须取指标表最新值")

	// Top 读取:响应全量字段中不出现资产表快照值 -1
	router := h.NewOSSRouter(1)
	status, env := osstest.GetJSON(t, router, "/assets/oss/top?days=7")
	require.Equal(t, 200, status)
	require.NotContains(t, env.Msg, "-1")
	raw, err := json.Marshal(env.Data)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(raw), ":-1"),
		"OSS 读取响应不得出现资产表快照数值: %s", string(raw))

	var top service.OSSTopResp
	require.NoError(t, json.Unmarshal(env.Data, &top))
	require.Len(t, top.Items, 1)
	require.NotNil(t, top.Items[0].Latest.StorageSize)
	require.Equal(t, float64(600), *top.Items[0].Latest.StorageSize)
}

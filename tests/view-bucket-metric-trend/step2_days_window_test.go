// @feature oss-ops-insight @api-functional
//
// Contract step-2-days-window: 指定天数窗口查看 — days within 1~90 are
// accepted with the series ascending over the exact requested window, and
// out-of-range days ( 0 or 91 ) are rejected with a 400 naming the bound.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package view_bucket_metric_trend

import (
	"context"
	"fmt"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome custom-window-accepted: days=30 请求趋势窗口 — 窗口内指标序列按日期
// 升序返回;days 在 1~90 范围内均被接受(fixture: 30 天连续行)。
func TestStep2_CustomWindowAccepted(t *testing.T) {
	h, provider := newJourneyHarness()
	seedTrendAccount(t, h, provider, 1)
	seedTrendWindow(t, h, provider, 1, 30, 100)

	resp, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, trendBucket, 30)
	require.NoError(t, err)
	require.Len(t, resp.Days, 30, "days=30 请求应返回 30 天窗口")
	// 序列按日期升序且首尾对齐请求窗口
	require.Equal(t, osstest.DayOffset(-29), resp.Days[0].Date)
	require.Equal(t, osstest.Today(), resp.Days[len(resp.Days)-1].Date)
	// 窗口内无缺失日(30 天连续落库)
	for _, point := range resp.Days {
		require.Equal(t, "ok", point.DataStatus)
		require.NotNil(t, point.StorageSize)
	}

	// 边界锚:days=1 与 days=90 均被接受
	for _, days := range []int{1, 90} {
		_, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, trendBucket, days)
		require.NoError(t, err, "days=%d 应被接受", days)
	}
}

// Outcome days-out-of-range-rejected: days=0 或 days=91 越出 1~90 — 参数校验
// 拒绝(400 类响应),明确提示 days 限 1~90;不越界查询。
func TestStep2_DaysOutOfRangeRejected(t *testing.T) {
	h, provider := newJourneyHarness()
	seedTrendAccount(t, h, provider, 1)

	router := h.NewOSSRouter(1)
	for _, days := range []string{"0", "-3", "91"} {
		status, env := osstest.GetJSON(t, router, trendPathRaw(1, days))
		require.Equal(t, 400, status, "days=%s 越界应返回 400", days)
		require.Contains(t, env.Msg, "days", "错误信息应指向 days 参数")
		require.Contains(t, env.Msg, "90", "错误信息应明确上限 90")
	}
}

// trendPathRaw 越界 days 用例专用路径构造(保留原始字符串)。
func trendPathRaw(accountID int64, days string) string {
	return fmt.Sprintf("/assets/oss/metrics?bucket_name=%s&account_id=%d&days=%s", trendBucket, accountID, days)
}

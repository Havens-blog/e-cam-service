// @feature disk-metrics-query @api-functional
//
// Contract disk-metrics-query step-3-qc-status-closure: the write-path
// qc_status=zero_exception is exposed verbatim in read responses and mapped
// into data_status ( the frontend can tell "usage 0 is an anomaly" from a
// normal idle disk ), and an empty window answers honestly with null values —
// never fake zeros.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_metrics_query

import (
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 写路径的 qc_status=zero_exception 在读取响应中原样透出
// ( qc_status 字段 ) 并映射进 data_status=zero_exception — 前端可分辨
// 「使用率 0 是异常」而非当正常空盘。
func TestStep3_QcStatusClosure_Success(t *testing.T) {
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	// 口径缺失 0 行(usage_scope 为空 = 厂商未提供使用率口径)
	zeroRow := disktest.Metric("dsk-zero", disktest.Today(), 0, 0, 0)
	zeroRow.UsageScope = ""
	h.SeedMetric(t, 1, provider.Name, zeroRow)

	router := h.NewDiskRouter(1)

	// 趋势面:qc_status 原样透出 + data_status 映射
	status, env := disktest.GetJSON(t, router, "/assets/disk/metrics?disk_id=dsk-zero&account_id=1&days=7")
	require.Equal(t, 200, status)
	var trend service.DiskMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &trend))
	for _, day := range trend.Days {
		if day.Date == disktest.Today() {
			require.Equal(t, "zero_exception", day.QcStatus, "qc_status 应原样透出落库标注")
			require.Equal(t, service.DiskDataStatusZeroException, day.DataStatus, "口径缺失 0 应映射 zero_exception")
			require.NotNil(t, day.UsagePercent)
			require.InDelta(t, 0, *day.UsagePercent, 1e-9)
		}
	}

	// Top 面:同一闭环
	status, env = disktest.GetJSON(t, router, "/assets/disk/top?account_id=1&days=7")
	require.Equal(t, 200, status)
	var top service.DiskTopResp
	require.NoError(t, json.Unmarshal(env.Data, &top))
	require.Len(t, top.Items, 1)
	require.Equal(t, "zero_exception", top.Items[0].QcStatus)
	require.Equal(t, service.DiskDataStatusZeroException, top.Items[0].DataStatus)
}

// Outcome empty-window-honest-null: 指定盘在请求 days 区间内无任何指标行 —
// 返回全 data_status=missing 标注的空态语义(区分「无数据」与「采集失败/
// 未启用」),不构造假值;近 N 天均值为空(null)而非 0。
func TestStep3_QcStatusClosure_EmptyWindowHonestNull(t *testing.T) {
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	// dsk-empty 在窗口内零指标行(新纳管盘形态)

	router := h.NewDiskRouter(1)
	status, env := disktest.GetJSON(t, router, "/assets/disk/metrics?disk_id=dsk-empty&account_id=1&days=7")
	require.Equal(t, 200, status)

	var resp service.DiskMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Equal(t, "dsk-empty", resp.DiskID)
	require.Len(t, resp.Days, 7)
	for _, day := range resp.Days {
		require.Equal(t, service.DiskDataStatusMissing, day.DataStatus, "空窗口应全量标注 missing")
		require.Nil(t, day.UsagePercent, "空窗口不构造假值")
		require.Nil(t, day.IOPS)
		require.Nil(t, day.Throughput)
	}
	// latest 为 null(无行);average 字段为 null 而非 0
	require.Nil(t, resp.Latest, "无行时 latest 应为 null")
	require.NotNil(t, resp.Average)
	require.Nil(t, resp.Average.UsagePercent, "无可用行时均值 usage_percent 应为 null 而非 0")
	require.Nil(t, resp.Average.IOPS)
	require.Nil(t, resp.Average.Throughput)
}

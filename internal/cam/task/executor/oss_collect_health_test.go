package executor

import (
	"context"
	"testing"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
)

// ==================== 任务 6:OSS 自我健康监控 测试 ====================
//
// 蓝本 nas_collect_observability_test.go(NAS 版健康监控测试)，OSS 平移:
// 必达厂商(aliyun/huawei/aws)近 3 天零成功写库行 + 该厂商存在 ≥1 个
// OSS bucket → 升级告警;仅全量运行判定，手动局部运行不误报。

// ossHealthDAOMock OSS 指标 DAO mock:可配置各厂商近 N 天成功写库行数
type ossHealthDAOMock struct {
	ossMetricDAOMock
	counts   map[string]int64
	countErr error
}

func (m *ossHealthDAOMock) CountMetricsByProviders(_ context.Context, providers []string, _ string) (map[string]int64, error) {
	if m.countErr != nil {
		return nil, m.countErr
	}
	out := make(map[string]int64, len(providers))
	for _, p := range providers {
		out[p] = m.counts[p]
	}
	return out, nil
}

// ossHealthAlertMock OSS 自我健康监控告警通道 mock(记录触发)
type ossHealthAlertCall struct {
	provider    string
	windowDays  int
	bucketCount int64
}

type ossHealthAlertMock struct {
	calls []ossHealthAlertCall
}

func (m *ossHealthAlertMock) AlertOSSZeroSuccess(_ context.Context, provider string, windowDays int, bucketCount int64) {
	m.calls = append(m.calls, ossHealthAlertCall{provider: provider, windowDays: windowDays, bucketCount: bucketCount})
}

// newOSSHealthTestExecutor 构造健康监控测试执行器(账号无 bucket，
// 采集主链路走 noOSSBuckets 短路，不影响健康判定)
func newOSSHealthTestExecutor(
	t *testing.T,
	accounts []domain.CloudAccount,
	counts map[string]int64,
	bucketsByProvider map[string][]camdomain.Instance,
	alert OSSHealthAlerter,
) *SyncOSSMetricsExecutor {
	t.Helper()
	e := NewSyncOSSMetricsExecutor(
		&cdnMetricAccountRepo{accounts: accounts},
		&healthInstanceRepoMock{byProvider: bucketsByProvider},
		&ossHealthDAOMock{counts: counts},
		&cdnMetricTaskRepo{},
		testLogger(),
	)
	e.SetOSSHealthAlerter(alert)
	return e
}

// 触发:huawei 近 3 天零成功且存在 bucket → 告警;aliyun 有成功行 → 抑制;
// aws 零成功但无 bucket → 抑制(Hard Rule 前置:无实盘桶不误报)
func TestOSSHealth_ZeroSuccessAlert(t *testing.T) {
	acc := ossMetricTestAccount(31, testOSSProviderWithMetrics)
	alert := &ossHealthAlertMock{}
	e := newOSSHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 5, "huawei": 0, "aws": 0},
		map[string][]camdomain.Instance{
			"aliyun": {ossTestBucket(31, "bucket-a"), ossTestBucket(31, "bucket-b")},
			"huawei": {ossTestBucket(31, "bucket-h")},
			"aws":    nil,
		},
		alert,
	)
	task := &taskx.Task{ID: "t6-oss-h1", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(alert.calls) != 1 {
		t.Fatalf("告警触发次数 = %d, want 1 (仅 huawei): %+v", len(alert.calls), alert.calls)
	}
	c := alert.calls[0]
	if c.provider != "huawei" || c.windowDays != 3 || c.bucketCount != 1 {
		t.Fatalf("告警参数错误: %+v", c)
	}
	alerts, _ := task.Result["health_alerts"].([]string)
	if len(alerts) != 1 || alerts[0] != "huawei" {
		t.Fatalf("health_alerts = %v, want [huawei]", task.Result["health_alerts"])
	}
}

// 抑制:各必达厂商均有成功写库行 → 不告警
func TestOSSHealth_SuppressWhenRowsExist(t *testing.T) {
	acc := ossMetricTestAccount(32, testOSSProviderWithMetrics)
	alert := &ossHealthAlertMock{}
	e := newOSSHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 1, "huawei": 2, "aws": 3},
		map[string][]camdomain.Instance{
			"aliyun": {ossTestBucket(32, "bucket-a")},
			"huawei": {ossTestBucket(32, "bucket-h")},
			"aws":    {ossTestBucket(32, "bucket-w")},
		},
		alert,
	)
	task := &taskx.Task{ID: "t6-oss-h2", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(alert.calls) != 0 {
		t.Fatalf("有成功行时不得告警: %+v", alert.calls)
	}
}

// 抑制:零成功但该厂商无任何 OSS bucket → 不告警(Hard Rule:防误报)
func TestOSSHealth_SuppressNoBuckets(t *testing.T) {
	acc := ossMetricTestAccount(33, testOSSProviderWithMetrics)
	alert := &ossHealthAlertMock{}
	e := newOSSHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 0, "huawei": 0, "aws": 0},
		nil, // 全部厂商无 bucket
		alert,
	)
	task := &taskx.Task{ID: "t6-oss-h3", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(alert.calls) != 0 {
		t.Fatalf("无 bucket 厂商不得触发零成功告警: %+v", alert.calls)
	}
}

// 仅全量运行判定:手动单账号运行不判定健康(避免以偏概全误报)
func TestOSSHealth_SkippedOnManualSingleAccountRun(t *testing.T) {
	acc := ossMetricTestAccount(34, testOSSProviderWithMetrics)
	alert := &ossHealthAlertMock{}
	e := newOSSHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 0, "huawei": 0, "aws": 0},
		map[string][]camdomain.Instance{
			"aliyun": {ossTestBucket(34, "bucket-a")},
		},
		alert,
	)
	task := &taskx.Task{ID: "t6-oss-h4", Params: map[string]any{"account_id": 34}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(alert.calls) != 0 {
		t.Fatalf("手动单账号运行不得触发健康告警: %+v", alert.calls)
	}
}

// 仅全量运行判定:手动单厂商运行同样不判定
func TestOSSHealth_SkippedOnManualSingleProviderRun(t *testing.T) {
	acc := ossMetricTestAccount(35, testOSSProviderWithMetrics)
	alert := &ossHealthAlertMock{}
	e := newOSSHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 0, "huawei": 0, "aws": 0},
		map[string][]camdomain.Instance{
			"aliyun": {ossTestBucket(35, "bucket-a")},
		},
		alert,
	)
	task := &taskx.Task{ID: "t6-oss-h5", Params: map[string]any{"provider": "aliyun"}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(alert.calls) != 0 {
		t.Fatalf("手动单厂商运行不得触发健康告警: %+v", alert.calls)
	}
}

// 未装配告警桥(最小装配):健康监控安全跳过，不 panic
func TestOSSHealth_NoAlerterWired(t *testing.T) {
	acc := ossMetricTestAccount(36, testOSSProviderWithMetrics)
	e := newOSSHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 0, "huawei": 0, "aws": 0},
		map[string][]camdomain.Instance{
			"aliyun": {ossTestBucket(36, "bucket-a")},
		},
		nil,
	)
	e.SetOSSHealthAlerter(nil) // 显式清空
	task := &taskx.Task{ID: "t6-oss-h6", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

// 健康窗口常量与日期口径:连续 3 天 = [今日-2, 今日](运营时区)
func TestOSSHealth_WindowConsts(t *testing.T) {
	if ossHealthWindowDays != 3 {
		t.Fatalf("ossHealthWindowDays = %d, want 3", ossHealthWindowDays)
	}
	want := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -2).Format("2006-01-02")
	got := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -(ossHealthWindowDays - 1)).Format("2006-01-02")
	if got != want {
		t.Fatalf("窗口起始日 = %s, want %s", got, want)
	}
}

package executor

import (
	"context"
	"testing"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
)

// ==================== 任务 6:Disk 自我健康监控 测试 ====================
//
// 蓝本 oss_collect_health_test.go(OSS 版健康监控测试)，Disk 平移:
// 必达厂商(aliyun/huawei/aws)近 3 天零成功写库行 + 该厂商存在 ≥1 个
// Disk 实例 → 升级告警;仅全量运行判定，手动局部运行不误报。

// diskHealthDAOMock Disk 指标 DAO mock:可配置各厂商近 N 天成功写库行数
type diskHealthDAOMock struct {
	diskMetricDAOMock
	counts   map[string]int64
	countErr error
}

func (m *diskHealthDAOMock) CountMetricsByProviders(_ context.Context, providers []string, _ string) (map[string]int64, error) {
	if m.countErr != nil {
		return nil, m.countErr
	}
	out := make(map[string]int64, len(providers))
	for _, p := range providers {
		out[p] = m.counts[p]
	}
	return out, nil
}

// diskHealthAlertMock Disk 自我健康监控告警通道 mock(记录触发)
type diskHealthAlertCall struct {
	provider      string
	windowDays    int
	instanceCount int64
}

type diskHealthAlertMock struct {
	calls []diskHealthAlertCall
}

func (m *diskHealthAlertMock) AlertDiskZeroSuccess(_ context.Context, provider string, windowDays int, instanceCount int64) {
	m.calls = append(m.calls, diskHealthAlertCall{provider: provider, windowDays: windowDays, instanceCount: instanceCount})
}

// newDiskHealthTestExecutor 构造健康监控测试执行器(账号无 Disk 实例，
// 采集主链路走 noDiskInstances 短路，不影响健康判定)
func newDiskHealthTestExecutor(
	t *testing.T,
	accounts []domain.CloudAccount,
	counts map[string]int64,
	disksByProvider map[string][]camdomain.Instance,
	alert DiskHealthAlerter,
) *SyncDiskMetricsExecutor {
	t.Helper()
	e := NewSyncDiskMetricsExecutor(
		&cdnMetricAccountRepo{accounts: accounts},
		&healthInstanceRepoMock{byProvider: disksByProvider},
		&diskHealthDAOMock{counts: counts},
		&cdnMetricTaskRepo{},
		testLogger(),
	)
	e.SetDiskHealthAlerter(alert)
	return e
}

// 触发:huawei 近 3 天零成功且存在 Disk 实例 → 告警;aliyun 有成功行 → 抑制;
// aws 零成功但无 Disk 实例 → 抑制(Hard Rule 前置:无实盘盘不误报)
func TestDiskHealth_ZeroSuccessAlert(t *testing.T) {
	acc := diskMetricTestAccount(41, testDiskProviderWithMetrics)
	alert := &diskHealthAlertMock{}
	e := newDiskHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 5, "huawei": 0, "aws": 0},
		map[string][]camdomain.Instance{
			"aliyun": {diskTestInstance(41, "disk-a", "cn-hangzhou")},
			"huawei": {diskTestInstance(41, "disk-h", "cn-beijing")},
			"aws":    nil,
		},
		alert,
	)
	task := &taskx.Task{ID: "t6-disk-h1", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(alert.calls) != 1 {
		t.Fatalf("告警触发次数 = %d, want 1 (仅 huawei): %+v", len(alert.calls), alert.calls)
	}
	c := alert.calls[0]
	if c.provider != "huawei" || c.windowDays != 3 || c.instanceCount != 1 {
		t.Fatalf("告警参数错误: %+v", c)
	}
	alerts, _ := task.Result["health_alerts"].([]string)
	if len(alerts) != 1 || alerts[0] != "huawei" {
		t.Fatalf("health_alerts = %v, want [huawei]", task.Result["health_alerts"])
	}
}

// 抑制:各必达厂商均有成功写库行 → 不告警
func TestDiskHealth_SuppressWhenRowsExist(t *testing.T) {
	acc := diskMetricTestAccount(42, testDiskProviderWithMetrics)
	alert := &diskHealthAlertMock{}
	e := newDiskHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 1, "huawei": 2, "aws": 3},
		map[string][]camdomain.Instance{
			"aliyun": {diskTestInstance(42, "disk-a", "cn-hangzhou")},
			"huawei": {diskTestInstance(42, "disk-h", "cn-beijing")},
			"aws":    {diskTestInstance(42, "disk-w", "us-east-1")},
		},
		alert,
	)
	task := &taskx.Task{ID: "t6-disk-h2", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(alert.calls) != 0 {
		t.Fatalf("有成功行时不得告警: %+v", alert.calls)
	}
}

// 抑制:零成功但该厂商无任何 Disk 实例 → 不告警(Hard Rule:防误报)
func TestDiskHealth_SuppressNoInstances(t *testing.T) {
	acc := diskMetricTestAccount(43, testDiskProviderWithMetrics)
	alert := &diskHealthAlertMock{}
	e := newDiskHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 0, "huawei": 0, "aws": 0},
		nil, // 全部厂商无 Disk 实例
		alert,
	)
	task := &taskx.Task{ID: "t6-disk-h3", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(alert.calls) != 0 {
		t.Fatalf("无 Disk 实例厂商不得触发零成功告警: %+v", alert.calls)
	}
}

// 仅全量运行判定:手动单账号运行不判定健康(避免以偏概全误报)
func TestDiskHealth_SkippedOnManualSingleAccountRun(t *testing.T) {
	acc := diskMetricTestAccount(44, testDiskProviderWithMetrics)
	alert := &diskHealthAlertMock{}
	e := newDiskHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 0, "huawei": 0, "aws": 0},
		map[string][]camdomain.Instance{
			"aliyun": {diskTestInstance(44, "disk-a", "cn-hangzhou")},
		},
		alert,
	)
	task := &taskx.Task{ID: "t6-disk-h4", Params: map[string]any{"account_id": 44}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(alert.calls) != 0 {
		t.Fatalf("手动单账号运行不得触发健康告警: %+v", alert.calls)
	}
}

// 仅全量运行判定:手动单厂商运行同样不判定
func TestDiskHealth_SkippedOnManualSingleProviderRun(t *testing.T) {
	acc := diskMetricTestAccount(45, testDiskProviderWithMetrics)
	alert := &diskHealthAlertMock{}
	e := newDiskHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 0, "huawei": 0, "aws": 0},
		map[string][]camdomain.Instance{
			"aliyun": {diskTestInstance(45, "disk-a", "cn-hangzhou")},
		},
		alert,
	)
	task := &taskx.Task{ID: "t6-disk-h5", Params: map[string]any{"provider": "aliyun"}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(alert.calls) != 0 {
		t.Fatalf("手动单厂商运行不得触发健康告警: %+v", alert.calls)
	}
}

// 未装配告警桥(最小装配):健康监控安全跳过，不 panic
func TestDiskHealth_NoAlerterWired(t *testing.T) {
	acc := diskMetricTestAccount(46, testDiskProviderWithMetrics)
	e := newDiskHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 0, "huawei": 0, "aws": 0},
		map[string][]camdomain.Instance{
			"aliyun": {diskTestInstance(46, "disk-a", "cn-hangzhou")},
		},
		nil,
	)
	e.SetDiskHealthAlerter(nil) // 显式清空
	task := &taskx.Task{ID: "t6-disk-h6", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

// 健康窗口常量与日期口径:连续 3 天 = [今日-2, 今日](运营时区)
func TestDiskHealth_WindowConsts(t *testing.T) {
	if diskHealthWindowDays != 3 {
		t.Fatalf("diskHealthWindowDays = %d, want 3", diskHealthWindowDays)
	}
	want := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -2).Format("2006-01-02")
	got := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -(diskHealthWindowDays - 1)).Format("2006-01-02")
	if got != want {
		t.Fatalf("窗口起始日 = %s, want %s", got, want)
	}
}

// 统计失败不反噬主链路:CountMetricsByProviders 报错时任务正常结束、不告警
func TestDiskHealth_CountErrorNotFailingTask(t *testing.T) {
	acc := diskMetricTestAccount(47, testDiskProviderWithMetrics)
	alert := &diskHealthAlertMock{}
	e := NewSyncDiskMetricsExecutor(
		&cdnMetricAccountRepo{accounts: []domain.CloudAccount{acc}},
		&healthInstanceRepoMock{byProvider: map[string][]camdomain.Instance{
			"aliyun": {diskTestInstance(47, "disk-a", "cn-hangzhou")},
		}},
		&diskHealthDAOMock{countErr: context.DeadlineExceeded},
		&cdnMetricTaskRepo{},
		testLogger(),
	)
	e.SetDiskHealthAlerter(alert)
	task := &taskx.Task{ID: "t6-disk-h7", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("统计失败不得使任务失败: %v", err)
	}
	if len(alert.calls) != 0 {
		t.Fatalf("统计失败不得触发告警: %+v", alert.calls)
	}
}

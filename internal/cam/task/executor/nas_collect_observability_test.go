package executor

import (
	"context"
	"fmt"
	"testing"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	camrepository "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-common-go/taskx"
)

// ==================== 任务 6:失败可观测 + 自我健康监控 测试 ====================

// 测试用厂商键(全局注册表,独特前缀防撞名)
const (
	testNASProviderFailing = domain.CloudProvider("nasmetric-fail")   // GetNASMetrics 一律报错(调用失败路径)
	testNASProviderEmpty   = domain.CloudProvider("nasmetric-empty")  // GetNASMetrics 返回空+nil(真实无数据路径)
	testNASProviderBroken  = domain.CloudProvider("nasmetric-broken") // CreateAdapter 直接失败(账号级失败)
)

// failingNAS 支持指标查询但调用必失败的 NAS 适配器 mock(探测支持、调用失败)
type failingNAS struct{}

func (n *failingNAS) ListInstances(_ context.Context, _ string) ([]types.NASInstance, error) {
	return nil, nil
}
func (n *failingNAS) GetInstance(_ context.Context, _, _ string) (*types.NASInstance, error) {
	return nil, nil
}
func (n *failingNAS) ListInstancesByIDs(_ context.Context, _ string, _ []string) ([]types.NASInstance, error) {
	return nil, nil
}
func (n *failingNAS) GetInstanceStatus(_ context.Context, _, _ string) (string, error) {
	return "Running", nil
}
func (n *failingNAS) ListInstancesWithFilter(_ context.Context, _ string, _ *types.NASInstanceFilter) ([]types.NASInstance, error) {
	return nil, nil
}
func (n *failingNAS) GetNASMetrics(_ context.Context, _, _, _, _, _ string) ([]types.NASMetric, error) {
	return nil, fmt.Errorf("mock: 厂商监控 API 调用失败(超时)")
}

var _ cloudx.NASAdapter = (*failingNAS)(nil)
var _ cloudx.NASMetricQuerier = (*failingNAS)(nil)

func init() {
	cloudx.RegisterAdapter(testNASProviderFailing, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &nasMetricCloudAdapter{provider: testNASProviderFailing, nas: &failingNAS{}}, nil
	})
	cloudx.RegisterAdapter(testNASProviderEmpty, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		// 真实无数据:适配器支持指标查询但实例无上报 → 空切片 + nil error
		return &nasMetricCloudAdapter{provider: testNASProviderEmpty, nas: &metricCapableNAS{metrics: []types.NASMetric{}}}, nil
	})
	cloudx.RegisterAdapter(testNASProviderBroken, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return nil, fmt.Errorf("mock: 创建适配器失败(凭证失效)")
	})
}

// healthDAOMock NAS 指标 DAO mock:记录写入 + 可配置各厂商近 N 天行数
type healthDAOMock struct {
	nasMetricDAOMock
	counts   map[string]int64
	countErr error
}

func (m *healthDAOMock) CountMetricsByProviders(_ context.Context, providers []string, _ string) (map[string]int64, error) {
	if m.countErr != nil {
		return nil, m.countErr
	}
	out := make(map[string]int64, len(providers))
	for _, p := range providers {
		out[p] = m.counts[p]
	}
	return out, nil
}

// healthInstanceRepoMock 实例仓储 mock:按厂商过滤(健康监控用,全租户口径)
type healthInstanceRepoMock struct {
	camrepository.InstanceRepository
	byProvider map[string][]camdomain.Instance
}

func (m *healthInstanceRepoMock) Search(_ context.Context, f camdomain.SearchFilter) ([]camdomain.Instance, int64, error) {
	if f.Provider != "" {
		insts := m.byProvider[f.Provider]
		n := int64(len(insts))
		if f.Limit > 0 && n > f.Limit {
			return insts[:f.Limit], n, nil
		}
		return insts, n, nil
	}
	// 账号维度枚举(采集主链路):健康测试里账号一律无实例即可
	return nil, 0, nil
}

// nasHealthAlertMock 自我健康监控告警通道 mock(记录触发)
type nasHealthAlertCall struct {
	provider      string
	windowDays    int
	instanceCount int64
}

type nasHealthAlertMock struct {
	calls []nasHealthAlertCall
}

func (m *nasHealthAlertMock) AlertNASZeroSuccess(_ context.Context, provider string, windowDays int, instanceCount int64) {
	m.calls = append(m.calls, nasHealthAlertCall{provider: provider, windowDays: windowDays, instanceCount: instanceCount})
}

// getFailures 从任务 Result 取失败明细
func getFailures(t *testing.T, task *taskx.Task) []nasProviderFailure {
	t.Helper()
	raw, ok := task.Result["failures"]
	if !ok {
		t.Fatal("Result 缺少 failures 键(失败明细必须随 Result 携带)")
	}
	failures, ok := raw.([]nasProviderFailure)
	if !ok {
		t.Fatalf("failures 类型错误: %T", raw)
	}
	return failures
}

// ---------- AC-1:失败计数写入 Result ----------

// 实例级调用失败:按厂商/账号累计 error_count 与 last_error,Result 携带
func TestSyncNASMetrics_FailureCountInResult(t *testing.T) {
	acc := nasMetricTestAccount(11, testNASProviderFailing)
	e, _ := newTestNASMetricsExecutor(t,
		[]domain.CloudAccount{acc},
		map[int64][]camdomain.Instance{
			11: {
				nasTestInstance(11, "fs-e1", "fs-e1", "cn-hangzhou"),
				nasTestInstance(11, "fs-e2", "fs-e2", "cn-hangzhou"),
			},
		},
	)
	task := &taskx.Task{ID: "t6-f1", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	failures := getFailures(t, task)
	if len(failures) != 1 {
		t.Fatalf("failures = %+v, want 1 条(该账号全部失败合并为一条)", failures)
	}
	f := failures[0]
	if f.Provider != string(testNASProviderFailing) || f.AccountID != 11 {
		t.Fatalf("失败明细厂商/账号错误: %+v", f)
	}
	if f.ErrorCount != 2 {
		t.Fatalf("error_count = %d, want 2(两个实例各失败一次)", f.ErrorCount)
	}
	if f.LastError == "" {
		t.Fatal("last_error 不得为空(末次错误必须可见)")
	}
	if got := task.Result["metrics_total"]; got != 0 {
		t.Fatalf("metrics_total = %v, want 0", got)
	}
}

// 写库失败同样计入失败明细(查询成功但落库失败,不得静默)
func TestSyncNASMetrics_WriteFailureCounted(t *testing.T) {
	acc := nasMetricTestAccount(12, testNASProviderWithMetrics)
	e, daoMock := newTestNASMetricsExecutor(t,
		[]domain.CloudAccount{acc},
		map[int64][]camdomain.Instance{
			12: {nasTestInstance(12, "fs-a", "fs-a-name", "cn-hangzhou")},
		},
	)
	daoMock.insertIfAbsentErr = fmt.Errorf("mock: mongo 写入失败")
	task := &taskx.Task{ID: "t6-f2", Params: map[string]any{"days": 1}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	failures := getFailures(t, task)
	if len(failures) != 1 || failures[0].ErrorCount != 1 {
		t.Fatalf("failures = %+v, want 1 条 error_count=1(写库失败计入)", failures)
	}
}

// 账号级失败(创建适配器失败)同样计入失败明细
func TestSyncNASMetrics_AccountLevelFailureCounted(t *testing.T) {
	acc := nasMetricTestAccount(13, testNASProviderBroken)
	e, _ := newTestNASMetricsExecutor(t,
		[]domain.CloudAccount{acc},
		map[int64][]camdomain.Instance{
			13: {nasTestInstance(13, "fs-b1", "fs-b1", "cn-hangzhou")},
		},
	)
	task := &taskx.Task{ID: "t6-f3", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	failures := getFailures(t, task)
	if len(failures) != 1 || failures[0].ErrorCount < 1 || failures[0].AccountID != 13 {
		t.Fatalf("failures = %+v, want 账号 13 的一条失败", failures)
	}
}

// 采集成功:failures 为空数组
func TestSyncNASMetrics_NoFailuresOnSuccess(t *testing.T) {
	acc := nasMetricTestAccount(14, testNASProviderWithMetrics)
	e, _ := newTestNASMetricsExecutor(t,
		[]domain.CloudAccount{acc},
		map[int64][]camdomain.Instance{
			14: {nasTestInstance(14, "fs-a", "fs-a-name", "cn-hangzhou")},
		},
	)
	task := &taskx.Task{ID: "t6-f4", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	failures := getFailures(t, task)
	if len(failures) != 0 {
		t.Fatalf("failures = %+v, want 空(成功采集不得产生失败明细)", failures)
	}
}

// ---------- AC-2:三条失败路径在执行器结果上可分辨 ----------

// 「真实无数据(空+nil)」与「探测不支持」都不产生失败明细;
// 「调用失败」必须产生失败明细——三者结果可分辨(Hard Rule)
func TestSyncNASMetrics_FailurePathsDistinguishable(t *testing.T) {
	emptyAcc := nasMetricTestAccount(15, testNASProviderEmpty)
	plainAcc := nasMetricTestAccount(16, testNASProviderWithoutMetrics)
	e, _ := newTestNASMetricsExecutor(t,
		[]domain.CloudAccount{emptyAcc, plainAcc},
		map[int64][]camdomain.Instance{
			15: {nasTestInstance(15, "fs-empty", "fs-empty", "cn-hangzhou")},
			16: {nasTestInstance(16, "fs-plain", "fs-plain", "cn-hangzhou")},
		},
	)
	task := &taskx.Task{ID: "t6-f5", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	failures := getFailures(t, task)
	if len(failures) != 0 {
		t.Fatalf("空数据/探测不支持不得计入失败明细: %+v", failures)
	}
	noSupport, _ := task.Result["no_metric_support"].([]string)
	if len(noSupport) != 1 || noSupport[0] != string(testNASProviderWithoutMetrics) {
		t.Fatalf("no_metric_support = %v, want [探测不支持厂商]", task.Result["no_metric_support"])
	}
}

// ---------- AC-3:自我健康监控(连续 3 天零成功 + ≥1 实例前置) ----------

func newHealthTestExecutor(
	t *testing.T,
	accounts []domain.CloudAccount,
	counts map[string]int64,
	instancesByProvider map[string][]camdomain.Instance,
	alert *nasHealthAlertMock,
) *SyncNASMetricsExecutor {
	t.Helper()
	daoMock := &healthDAOMock{counts: counts}
	e := NewSyncNASMetricsExecutor(
		&cdnMetricAccountRepo{accounts: accounts},
		&healthInstanceRepoMock{byProvider: instancesByProvider},
		daoMock,
		&cdnMetricTaskRepo{},
		testLogger(),
	)
	e.SetNASHealthAlerter(alert)
	return e
}

// 触发:huawei 近 3 天零成功且存在实例 → 告警;aliyun 有成功行 → 抑制;
// aws 零成功但无实例 → 抑制(Hard Rule 前置)
func TestNASHealth_ZeroSuccessAlert(t *testing.T) {
	acc := nasMetricTestAccount(21, testNASProviderWithMetrics)
	alert := &nasHealthAlertMock{}
	e := newHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 5, "huawei": 0, "aws": 0},
		map[string][]camdomain.Instance{
			"aliyun": {nasTestInstance(21, "fs-a", "fs-a", "cn-hangzhou"), nasTestInstance(21, "fs-a2", "fs-a2", "cn-hangzhou"), nasTestInstance(21, "fs-a3", "fs-a3", "cn-hangzhou")},
			"huawei": {nasTestInstance(21, "fs-h", "fs-h", "cn-north-4")},
			"aws":    nil,
		},
		alert,
	)
	task := &taskx.Task{ID: "t6-h1", Params: map[string]any{}}
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

// 抑制:各必达厂商均有成功行 → 不告警
func TestNASHealth_SuppressWhenRowsExist(t *testing.T) {
	acc := nasMetricTestAccount(22, testNASProviderWithMetrics)
	alert := &nasHealthAlertMock{}
	e := newHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 1, "huawei": 2, "aws": 3},
		map[string][]camdomain.Instance{
			"aliyun": {nasTestInstance(22, "fs-a", "fs-a", "cn-hangzhou")},
			"huawei": {nasTestInstance(22, "fs-h", "fs-h", "cn-north-4")},
			"aws":    {nasTestInstance(22, "fs-w", "fs-w", "us-east-1")},
		},
		alert,
	)
	task := &taskx.Task{ID: "t6-h2", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(alert.calls) != 0 {
		t.Fatalf("有成功行时不得告警: %+v", alert.calls)
	}
}

// 抑制:零成功但该厂商无任何 NAS 实例 → 不告警(Hard Rule:防无实例厂商误报)
func TestNASHealth_SuppressNoInstances(t *testing.T) {
	acc := nasMetricTestAccount(23, testNASProviderWithMetrics)
	alert := &nasHealthAlertMock{}
	e := newHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 0, "huawei": 0, "aws": 0},
		nil, // 全部厂商无实例
		alert,
	)
	task := &taskx.Task{ID: "t6-h3", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(alert.calls) != 0 {
		t.Fatalf("无实例厂商不得触发零成功告警: %+v", alert.calls)
	}
}

// 手动单账号运行不判定健康(避免以单账号结果以偏概全)
func TestNASHealth_SkippedOnManualSingleAccountRun(t *testing.T) {
	acc := nasMetricTestAccount(24, testNASProviderWithMetrics)
	alert := &nasHealthAlertMock{}
	e := newHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 0, "huawei": 0, "aws": 0},
		map[string][]camdomain.Instance{
			"aliyun": {nasTestInstance(24, "fs-a", "fs-a", "cn-hangzhou")},
		},
		alert,
	)
	task := &taskx.Task{ID: "t6-h4", Params: map[string]any{"account_id": 24}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(alert.calls) != 0 {
		t.Fatalf("手动单账号运行不得触发健康告警: %+v", alert.calls)
	}
}

// 未装配告警桥(最小装配):健康监控安全跳过,不 panic
func TestNASHealth_NoAlerterWired(t *testing.T) {
	acc := nasMetricTestAccount(25, testNASProviderWithMetrics)
	e := newHealthTestExecutor(t,
		[]domain.CloudAccount{acc},
		map[string]int64{"aliyun": 0, "huawei": 0, "aws": 0},
		map[string][]camdomain.Instance{
			"aliyun": {nasTestInstance(25, "fs-a", "fs-a", "cn-hangzhou")},
		},
		nil,
	)
	e.SetNASHealthAlerter(nil) // 显式清空
	task := &taskx.Task{ID: "t6-h5", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

// 健康窗口常量与日期口径:连续 3 天 = [今日-2, 今日](运营时区)
func TestNASHealth_WindowConsts(t *testing.T) {
	if nasHealthWindowDays != 3 {
		t.Fatalf("nasHealthWindowDays = %d, want 3", nasHealthWindowDays)
	}
	want := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -2).Format("2006-01-02")
	got := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -(nasHealthWindowDays - 1)).Format("2006-01-02")
	if got != want {
		t.Fatalf("窗口起始日 = %s, want %s", got, want)
	}
}

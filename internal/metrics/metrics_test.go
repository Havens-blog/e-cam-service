package metrics

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// gatherNames 收集默认 registry 中已注册的全部指标族名。
func gatherNames(t *testing.T) map[string]bool {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather 默认 registry 失败: %v", err)
	}
	names := make(map[string]bool, len(families))
	for _, mf := range families {
		names[mf.GetName()] = true
	}
	return names
}

// labelNamesOf 取指标族首个样本的标签名集合（需先制造样本）。
func labelNamesOf(t *testing.T, name string) map[string]bool {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather 默认 registry 失败: %v", err)
	}
	for _, mf := range families {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := make(map[string]bool, len(m.GetLabel()))
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = true
			}
			return labels
		}
	}
	return nil
}

func assertLabelNames(t *testing.T, name string, want []string) {
	t.Helper()
	labels := labelNamesOf(t, name)
	if labels == nil {
		t.Fatalf("指标 %s 无样本可断言标签", name)
	}
	for _, l := range want {
		if !labels[l] {
			t.Errorf("指标 %s 缺少标签 %q（现有标签集 %v）", name, l, labels)
		}
	}
}

// TestMetricsRegisteredInDefaultRegistry 用重复注册必须报
// AlreadyRegisteredError 来断言 4 个指标已挂默认 registry。
// 注：*Vec 无子序列时 Gather 不输出指标族（元数据来自采集到的样本而非
// 注册动作），故不能用"是否出现在 Gather 结果"判断注册与否。
func TestMetricsRegisteredInDefaultRegistry(t *testing.T) {
	for _, c := range []prometheus.Collector{
		HTTPRequestsTotal, HTTPRequestDuration, SyncTaskTotal, SyncTaskDuration,
	} {
		err := prometheus.Register(c)
		var are prometheus.AlreadyRegisteredError
		if !errors.As(err, &are) {
			t.Errorf("指标 %T 未注册到默认 registry（重复注册返回 %v，期望 AlreadyRegisteredError）", c, err)
		}
	}
	// 语义名最终校验：制造样本后 Gather 结果中应出现 4 个 ecam_* 族
	HTTPRequestsTotal.WithLabelValues("GET", "/__probe__", "200").Inc()
	HTTPRequestDuration.WithLabelValues("GET", "/__probe__").Observe(0.01)
	SyncTaskTotal.WithLabelValues("cam:__probe__", "success").Inc()
	SyncTaskDuration.WithLabelValues("cam:__probe__").Observe(0.01)
	names := gatherNames(t)
	for _, want := range []string{
		"ecam_http_requests_total",
		"ecam_http_request_duration_seconds",
		"ecam_sync_task_total",
		"ecam_sync_task_duration_seconds",
	} {
		if !names[want] {
			t.Errorf("指标族 %s 未出现在默认 registry Gather 结果", want)
		}
	}
}

func TestHTTPMetricLabelSets(t *testing.T) {
	HTTPRequestsTotal.Reset()
	HTTPRequestDuration.Reset()

	HTTPRequestsTotal.WithLabelValues("GET", "/api/v1/x", "200").Inc()
	HTTPRequestDuration.WithLabelValues("GET", "/api/v1/x").Observe(0.1)

	if got := testutil.CollectAndCount(HTTPRequestsTotal); got != 1 {
		t.Errorf("ecam_http_requests_total 样本数 = %d, want 1", got)
	}
	if got := testutil.CollectAndCount(HTTPRequestDuration); got < 1 {
		t.Errorf("ecam_http_request_duration_seconds 未采集到样本")
	}

	assertLabelNames(t, "ecam_http_requests_total", []string{"method", "route", "status"})
	assertLabelNames(t, "ecam_http_request_duration_seconds", []string{"method", "route"})
}

func TestSyncMetricLabelSets(t *testing.T) {
	SyncTaskTotal.Reset()
	SyncTaskDuration.Reset()

	SyncTaskTotal.WithLabelValues("cam:sync_assets", "success").Inc()
	SyncTaskDuration.WithLabelValues("cam:sync_assets").Observe(1.5)

	if got := testutil.CollectAndCount(SyncTaskTotal); got != 1 {
		t.Errorf("ecam_sync_task_total 样本数 = %d, want 1", got)
	}
	if got := testutil.CollectAndCount(SyncTaskDuration); got < 1 {
		t.Errorf("ecam_sync_task_duration_seconds 未采集到样本")
	}

	assertLabelNames(t, "ecam_sync_task_total", []string{"task_type", "result"})
	assertLabelNames(t, "ecam_sync_task_duration_seconds", []string{"task_type"})
}

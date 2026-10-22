package metrics

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type fakeExecutor struct {
	typ taskx.TaskType
	err error
}

func (f *fakeExecutor) Execute(ctx context.Context, task *taskx.Task) error {
	return f.err
}

func (f *fakeExecutor) GetType() taskx.TaskType {
	return f.typ
}

// TestInstrumentExecutorSuccess 验证成功路径：result=success 计数 +1，
// 耗时直方图采集到样本。
func TestInstrumentExecutorSuccess(t *testing.T) {
	SyncTaskTotal.Reset()
	SyncTaskDuration.Reset()

	inner := &fakeExecutor{typ: "cam:test_ok"}
	wrapped := InstrumentExecutor(inner)

	if err := wrapped.Execute(context.Background(), &taskx.Task{Type: "cam:test_ok"}); err != nil {
		t.Fatalf("Execute 返回错误: %v", err)
	}
	if got := testutil.ToFloat64(SyncTaskTotal.WithLabelValues("cam:test_ok", "success")); got != 1 {
		t.Errorf("ecam_sync_task_total{task_type=cam:test_ok,result=success} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(SyncTaskTotal.WithLabelValues("cam:test_ok", "failure")); got != 0 {
		t.Errorf("成功任务被记为 failure: %v", got)
	}
	if got := testutil.CollectAndCount(SyncTaskDuration); got < 1 {
		t.Errorf("ecam_sync_task_duration_seconds 未采集到样本")
	}
}

// TestInstrumentExecutorFailure 验证失败路径：result=failure 计数 +1，
// 且底层错误原样透传（装饰器不得吞错）。
func TestInstrumentExecutorFailure(t *testing.T) {
	SyncTaskTotal.Reset()
	SyncTaskDuration.Reset()

	wantErr := errors.New("cloud api down")
	inner := &fakeExecutor{typ: "cam:test_fail", err: wantErr}
	wrapped := InstrumentExecutor(inner)

	gotErr := wrapped.Execute(context.Background(), &taskx.Task{Type: "cam:test_fail"})
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("错误未透传: got %v, want %v", gotErr, wantErr)
	}
	if got := testutil.ToFloat64(SyncTaskTotal.WithLabelValues("cam:test_fail", "failure")); got != 1 {
		t.Errorf("ecam_sync_task_total{task_type=cam:test_fail,result=failure} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(SyncTaskTotal.WithLabelValues("cam:test_fail", "success")); got != 0 {
		t.Errorf("失败任务被记为 success: %v", got)
	}
}

// TestInstrumentExecutorGetTypePassthrough 验证 GetType 透传，
// taskx.Queue 按类型分发依赖该值。
func TestInstrumentExecutorGetTypePassthrough(t *testing.T) {
	inner := &fakeExecutor{typ: "cam:test_type"}
	wrapped := InstrumentExecutor(inner)
	if got := wrapped.GetType(); got != "cam:test_type" {
		t.Errorf("GetType() = %q, want %q", got, "cam:test_type")
	}
}

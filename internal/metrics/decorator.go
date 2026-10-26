package metrics

import (
	"context"
	"time"

	"github.com/Havens-blog/e-common-go/taskx"
)

// instrumentedExecutor TaskExecutor 的指标装饰器：包装任意执行器，按
// task_type + result(success|failure) 统计执行次数与耗时。
//
// task_type 取被装饰执行器的 GetType()——与 taskx.Queue 注册分发用的类型
// 字符串同源（如 cam:sync_assets），不动态拼接租户/账号，保持低基数。
type instrumentedExecutor struct {
	next taskx.TaskExecutor
}

// InstrumentExecutor 用 Prometheus 指标装饰任务执行器。
// 装饰在 task 模块的执行器注册处进行（ioc/cam 组合根侧），不修改
// e-common-go/taskx 第三方库。
func InstrumentExecutor(next taskx.TaskExecutor) taskx.TaskExecutor {
	return &instrumentedExecutor{next: next}
}

// Execute 透传执行并记录 result 与耗时；错误原样返回，装饰器不吞错。
func (e *instrumentedExecutor) Execute(ctx context.Context, task *taskx.Task) error {
	start := time.Now()
	err := e.next.Execute(ctx, task)

	taskType := string(e.next.GetType())
	result := "success"
	if err != nil {
		result = "failure"
	}
	SyncTaskTotal.WithLabelValues(taskType, result).Inc()
	SyncTaskDuration.WithLabelValues(taskType).Observe(time.Since(start).Seconds())
	return err
}

// GetType 透传被装饰执行器的任务类型，taskx.Queue 按该值分发任务。
func (e *instrumentedExecutor) GetType() taskx.TaskType {
	return e.next.GetType()
}

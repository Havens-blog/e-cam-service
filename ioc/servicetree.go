package ioc

import (
	"github.com/Havens-blog/e-cam-service/internal/cam"
)

// WireRuleExecutor 将服务树规则引擎注入资产同步执行器（资产同步完成后自动执行规则，
// 事件驱动挂点，一期方案 4）。与 WireChangeTracker 同款显式接线模式：
// wire 不支持无返回值副作用调用，故在此显式接线（wire_gen.go 调用）。
// executor 经接口注入，不直接 import servicetree，避免依赖环。
func WireRuleExecutor(camModule *cam.Module) {
	if camModule.TaskModule == nil || camModule.ServiceTreeModule == nil || camModule.ServiceTreeModule.RuleService == nil {
		return
	}
	camModule.TaskModule.SetRuleExecutor(camModule.ServiceTreeModule.RuleService)
}

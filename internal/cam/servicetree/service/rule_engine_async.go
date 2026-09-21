package service

import (
	"context"
	"time"

	"github.com/gotomicro/ego/core/elog"
)

// ruleAsyncTimeout 单次规则自动执行的超时上限。
// 同步后自动执行属后台任务：超时放弃，不拖住同步链路，也不无限占用 goroutine。
var ruleAsyncTimeout = 2 * time.Minute

// ExecuteRulesAsync 异步执行规则引擎（资产同步完成后的事件驱动挂点，一期方案 4）。
// 不阻塞调用方：内部 goroutine + panic recover + 超时 + 失败仅日志（对齐 CDN 指标采集教训：
// 后台任务失败不向上传播、不阻塞主链路）。同租户已有执行在跑时跳过本次触发（in-flight 去重）。
// 绑定幂等由 ExecuteRules 的资源级去重 + DB 资源级唯一键共同保证。
func (s *ruleEngineService) ExecuteRulesAsync(tenantID int64) {
	// in-flight 去重：同租户并发触发（多次同步完成事件/手动+自动撞车）只放行一个执行
	if _, busy := s.asyncRunning.LoadOrStore(tenantID, struct{}{}); busy {
		s.logger.Info("规则自动执行跳过：同租户已在执行中", elog.Int64("tenantID", tenantID))
		return
	}

	go func() {
		defer s.asyncRunning.Delete(tenantID)
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("规则自动执行 panic 已恢复（不影响同步结果）",
					elog.Int64("tenantID", tenantID),
					elog.Any("panic", r))
			}
		}()

		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), ruleAsyncTimeout)
		defer cancel()

		count, err := s.ExecuteRules(ctx, tenantID)
		cost := time.Since(start)
		if err != nil {
			// 失败仅日志：同步已返回，规则执行失败不能也不需要向上传播
			s.logger.Error("同步后规则自动执行失败",
				elog.Int64("tenantID", tenantID),
				elog.Int64("newBindingCount", count),
				elog.String("cost", cost.String()),
				elog.FieldErr(err))
			return
		}
		s.logger.Info("同步后规则自动执行完成",
			elog.Int64("tenantID", tenantID),
			elog.Int64("newBindingCount", count),
			elog.String("cost", cost.String()))
	}()
}

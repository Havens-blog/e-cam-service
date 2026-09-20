---
status: "completed"
started: "2026-09-19 18:23"
completed: "2026-09-19 18:37"
time_spent: "~14m"
---

# Task Record: 8 CDN 日闸迁移到持久化 + 值回归 + 特性开关回滚

## Summary
CDN 每日指标采集从内存闸 lastMetricsCollectDate 迁移到 T7 持久化日闸 cdn 键(checkMetricsCollectionPersistent,findOneAndUpdate 原子认领,认领成功才提交,首部署无 cdn 记录按首次认领→当日一次过渡);新增特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED(feature_flag.go,默认开启,仅显式 false/0/off 关闭),关闭时 NAS/CDN 一并回滚内存闸(checkNASMetricsCollectionMemory/checkMetricsCollectionMemory,调度可用性优先,内存字段保留兼容);wire.go 注入开关解析;补 9 个单测覆盖迁移值回归(内存闸基线→持久化无缺口/无历史重采/days=2 不变)、首部署过渡、重启×3 仅 1 条、写失败退避+cdn 告警、读失败 ≥5 分钟退避、回滚后 NAS/CDN 调度仍正常提交

## Changes

### Files Created
- internal/cam/scheduler/feature_flag.go
- internal/cam/scheduler/auto_sync_metrics_gate_test.go

### Files Modified
- internal/cam/scheduler/auto_sync.go
- internal/cam/scheduler/auto_sync_metrics.go
- internal/cam/scheduler/auto_sync_nas_metrics.go
- internal/cam/scheduler/daily_gate.go
- internal/cam/scheduler/auto_sync_nas_metrics_test.go
- internal/cam/wire.go

### Key Decisions
- 回滚开关在调度器内部分流(checkMetricsCollection 分支)而非装配层摘除 dailyGate:回滚是运行时一键行为,且 NAS/CDN 必须一并回滚(共用同一开关)
- 内存闸时间源从 time.Now() 收敛到 s.nowFn(既有时钟注入点),使多日跨日场景可单测,生产行为不变
- 提交失败不回滚认领(与 NAS T7 语义一致):回滚认领会与多副本认领竞争,队列 Submit 失败属运维级故障,ERROR 留痕
- CDN 开关开启但 dailyGate 为 nil 时安全跳过不降级内存闸(与 NAS 一致,防半吊子回退让重启重复提交缺陷回归)

## Test Results
- **Tests Executed**: Yes
- **Passed**: 25
- **Failed**: 0
- **Coverage**: 56.1%

## Acceptance Criteria
- [x] CDN checkMetricsCollection 从读内存 lastMetricsCollectDate 改为读持久化日闸 cdn 键;auto_sync.go 内存字段保留兼容(仅回滚路径使用)
- [x] CDN 迁移值回归:同域同日值无缺口、无重复历史,days=2 语义不变;首部署无 cdn 记录按首次认领→触发一次当日提交过渡
- [x] 特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 默认开启;切回内存闸后 NAS/CDN 调度任务仍正常提交、指标采集不中断
- [x] 连续 3 次重启 CDN 任务各仅 1 条(累计 1);写失败注入退避+升级告警(cdn/write);读失败注入 ≥5 分钟退避窗口内不重复提交
- [x] 单测覆盖迁移值回归/首部署过渡/开关回滚;go build ./... 通过

## Notes
AC4 的『连续 3 次重启』以单测模拟重启(同一 scheduler_state 存储上重建调度器+日闸)验证;实机重启×3 观察属上线验证步骤(部署后执行)。静态检查:go build OK、gofmt 无差异、go vet ./internal/cam/... OK;golangci-lint 本机未安装(环境既有状况)。覆盖率 56.1% 为 scheduler 包总量(拖累项为既有未测的 run/Start/Stop 循环代码,本任务未触碰),本任务改动函数覆盖率 83.3%~100%。

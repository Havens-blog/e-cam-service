---
status: "completed"
started: "2026-09-19 17:32"
completed: "2026-09-19 17:49"
time_spent: "~17m"
---

# Task Record: 6 采集失败可观测 + 自我健康监控(失败计数入 Result + 连续零成功告警)

## Summary
NAS 采集失败可观测 + 自我健康监控:①执行器新增 nasProviderFailure{provider,account_id,error_count,last_error} 并发安全累计器,实例级(查询/写库失败)与账号级(适配器创建/实例枚举失败)失败合并为厂商/账号维度明细,任务 Result 以 failures 键携带(扩展 CDN skipped_providers 雏形),采集成功时为空数组;②失败路径三分:探测不支持(INFO,no_metric_support)与真实无数据(空+nil)不计失败,调用失败(ERROR+error 字段)计入 failures——适配器侧分级 T3/T4 已落地并有单测,本任务补齐执行器侧可分辨语义;③自我健康监控挂在每日采集完成钩子:必达厂商(aliyun/huawei/aws,T1 probe-report §4 定案)近 3 天([今日-2,今日],运营时区)零成功写库行(查 ecam_nas_metric 行存在为成功证据)且该厂商 ecam_instance 实盘存在 ≥1 个 NAS 实例(Hard Rule 前置,防无实例厂商误报)→ 经与持久化日闸共用的同一告警桥(schedulerGateAlerter,同一定义勿重复造)落 alert DAO critical 级事件(绕过规则匹配直落库),手动单账号/单厂商运行不判定;新增 NASMetricDAO.CountMetricsByProviders 只读方法,module.go/wire.go 完成 SetNASHealthAlerter 装配(与日闸告警共用同一实例)

## Changes

### Files Created
- internal/cam/task/executor/nas_health_monitor.go
- internal/cam/task/executor/nas_collect_observability_test.go

### Files Modified
- internal/cam/task/executor/sync_nas_metrics.go
- internal/cam/repository/dao/nas_metric.go
- internal/cam/scheduler_gate_alerter.go
- internal/cam/wire.go
- internal/cam/task/module.go

### Key Decisions
- 失败明细结构 nasProviderFailure 按 (provider, account_id) 归并并保留 error_count/last_error,而非逐实例堆叠——运营视角按厂商/账号排查,Result 体积可控
- 告警桥复用 cam.schedulerGateAlerter:构造函数改返回具体类型 *schedulerGateAlerter,同时满足 scheduler.DailyGateAlerter 与 executor.NASHealthAlerter 两接口,同一实例装配日闸与健康监控(Hard Rule:同一定义勿重复造)
- 健康监控零成功判定以「窗口内 ecam_nas_metric 行存在」为成功证据,不依赖任务状态(行只会在成功写库时产生);因触发条件已蕴含连续 3 天静默失效,告警直接落 critical 不走 warning 逐级
- 健康监控仅在全量运行(AccountID=0 且 Provider 空)判定,手动单账号/单厂商运行以偏概全会误报;监控自身故障(统计失败)只记日志,不反噬采集主链路
- 成功采集的证据窗口含今日([今日-2,今日]):日闸认领后必跑全量采集,成功则今日行已落库,与 days=2 采集窗口语义对齐

## Test Results
- **Tests Executed**: Yes
- **Passed**: 19
- **Failed**: 0
- **Coverage**: 61.3%

## Acceptance Criteria
- [x] 执行器按厂商/账号维护失败计数与末次错误,任务 Result 携带(扩展 CDN skipped_providers 雏形为含错误明细的结构,运营可查)
- [x] 适配器失败路径可分辨:探测不支持 → INFO;调用失败返回空 → ERROR + error 字段(不静默)
- [x] 自我健康监控:必达厂商(以 T1 分组为准)连续 3 天零成功采集、且该厂商实盘存在 ≥1 个 NAS 实例 → 升级告警(与日闸告警通道共用);无 NAS 实例厂商不触发
- [x] 单测:失败计数写入 Result、探测不支持 vs 调用失败的日志分级、连续零成功告警触发/抑制条件
- [x] go build ./... 通过

## Notes
新增 11 个单测(失败计数 4 + 失败路径三分 1 + 健康监控 5 + 常量口径 1),连同 T5 既有 8 个执行器单测全绿(19/19);触及函数覆盖率 80%~100%(包整体 61.3% 由本任务未触碰的其他执行器拉低);gofmt/go vet 通过,golangci-lint 本机未安装(Makefile lint 目标本就 command -v 优雅降级);DAO CountMetricsByProviders 为薄 CountDocuments 封装,经执行器侧 mock 全覆盖,未单测 mongo 实现;风险提示(proposal):前端消费 failures/health_alerts 的空态区分在 T11 落地

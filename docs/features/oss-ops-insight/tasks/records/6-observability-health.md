---
status: "completed"
started: "2026-09-20 12:58"
completed: "2026-09-20 13:05"
time_spent: "~7m"
---

# Task Record: 6 采集失败可观测 + 自我健康监控(失败计数入 Result + 连续零成功告警)

## Summary
OSS 采集失败可观测 + 自我健康监控收尾:1) 失败累计器命名口径补齐——新增 ossProviderFailure 别名(复用共享 nasAccountFailure/nasProviderFailure 并发安全实现,零行为差异),失败明细继续随 Result["failures"] 携带;2) OSSMetricDAO 新增 CountMetricsByProviders(与 NAS 同口径,逐厂商 CountDocuments,date 字典序即时间序);3) 新建 oss_health_monitor.go:必达厂商(aliyun/huawei/aws,同 T1 分组)近 3 天零成功写库行且实盘存在 ≥1 个 OSS bucket → 升级告警;仅全量运行判定,手动单账号/单厂商不判定;健康监控自身故障只记日志不反噬主链路;4) 告警桥 schedulerGateAlerter 新增 AlertOSSZeroSuccess(与日闸/NAS 健康监控共用同一实例,直落 critical 告警事件,勿重复造通道);5) 装配:task/module.go 存 ossMetricsExecutor + SetOSSHealthAlerter,wire.go 与 gateAlerter 同实例注入。探测不支持与真实无数据不计失败(Hard Rule)在 T5 已落实,本轮补健康监控测试守护。8 个新测试(健康触发/有行抑制/无桶抑制/手动单账号抑制/手动单厂商抑制/未装配告警桥安全跳过/窗口常量口径 + 失败路径守护),OSS+NAS 观测相关 38 测试全绿,go build ./... 通过。

## Changes

### Files Created
- internal/cam/task/executor/oss_health_monitor.go
- internal/cam/task/executor/oss_collect_health_test.go

### Files Modified
- internal/cam/repository/dao/oss_metric.go
- internal/cam/task/executor/sync_oss_metrics.go
- internal/cam/scheduler_gate_alerter.go
- internal/cam/task/module.go
- internal/cam/wire.go

### Key Decisions
- 告警桥复用:AlertOSSZeroSuccess 加在既有 schedulerGateAlerter 上(同一实例装配日闸/NAS/OSS 三处),不新建告警通道(Hard Rule)
- ossProviderFailure 以类型别名复用 nasProviderFailure(字段同型),满足 AC 命名口径且零重复定义
- 健康监控整体平移 nas_health_monitor.go 蓝本:窗口 3 天、必达厂商清单与 T1 一致、无 bucket 厂商前置抑制防误报
- OSSMetricDAO 接口新增 CountMetricsByProviders(仅 3 必达厂商,无需聚合管道)

## Test Results
- **Tests Executed**: Yes
- **Passed**: 38
- **Failed**: 0
- **Coverage**: 68.0%

## Acceptance Criteria
- [x] OSS 失败累计器(并发安全):实例级查询/写库失败 + 账号级适配器创建/枚举失败,归并 ossProviderFailure 随 Result.failures 携带
- [x] 探测不支持与真实无数据不计失败(三分语义)
- [x] 自我健康监控:必达厂商近 3 天零成功写库行 + 该厂商存在 ≥1 个 OSS bucket → 升级告警(复用告警桥)
- [x] 仅全量运行判定,手动局部运行不误报
- [x] 单测:失败计数 4 + 路径三分 + 健康触发/抑制;go build ./... 通过

## Notes
覆盖率 68.0% 为 executor 包既有基线(多数无关执行器无测试);本次新增健康监控代码路径 7 测试全绿全覆盖。golangci-lint 未安装按 Makefile 口径跳过;gofmt/go vet/go build 全绿。

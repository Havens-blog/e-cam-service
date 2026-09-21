---
status: "completed"
started: "2026-09-21 12:29"
completed: "2026-09-21 12:39"
time_spent: "~10m"
---

# Task Record: 6 采集失败可观测 + 自我健康监控(失败计数入 Result + 连续零成功告警)

## Summary
Disk 采集失败可观测 + 自我健康监控收尾:必达厂商(aliyun/huawei/aws)近 3 天零成功写库行且实盘存在 ≥1 个 Disk 实例时经共用告警桥升级告警。新增 DiskMetricDAO.CountMetricsByProviders(与 NAS/OSS 同名同签名)、disk_health_monitor.go(OSS 蓝本平移:DiskHealthAlerter 接口/SetDiskHealthAlerter/checkMandatoryProviderHealth 仅全量运行判定/countDiskInstances 前置核验)、schedulerGateAlerter.AlertDiskZeroSuccess(critical 直落库)、cam/task/module.go SetDiskHealthAlerter 与 cam/wire.go 装配(同一 gateAlerter 实例,勿重复造);Result 新增 health_alerts。失败累计器(实例级查询/写库 + 账号级适配器创建/枚举失败归并 diskProviderFailure 入 Result.failures)与三分语义(探测不支持/真实无数据不计失败)任务 5 已落位,本任务补验既有测试。

## Changes

### Files Created
- internal/cam/task/executor/disk_health_monitor.go
- internal/cam/task/executor/disk_collect_health_test.go

### Files Modified
- internal/cam/repository/dao/disk_metric.go
- internal/cam/task/executor/sync_disk_metrics.go
- internal/cam/scheduler_gate_alerter.go
- internal/cam/task/module.go
- internal/cam/wire.go

### Key Decisions
- 健康监控按 oss_health_monitor.go 蓝本整体平移(Disk 第三次同型平移),必达厂商清单/窗口常量直接复用 NAS 共享定义(nasMandatoryProviders/nasHealthWindowDays)
- AlertDiskZeroSuccess 落 alert 域 scheduler:disk-health:<provider>,critical 直落库,与日闸/NAS/OSS 健康告警共用同一 schedulerGateAlerter 实例(Hard Rule 勿重复造告警通道)
- 告警前置「该厂商实盘存在 ≥1 个 Disk 实例」由执行器核验(countDiskInstances,全租户口径 Limit=1 取 total),无实例厂商零成功属预期不误报(Hard Rule)
- 仅全量运行判定(AccountID/Provider 均未指定),手动单账号/单厂商运行不判定防误报;健康监控统计失败只记日志不反噬采集主链路

## Test Results
- **Tests Executed**: Yes
- **Passed**: 213
- **Failed**: 0
- **Coverage**: 91.3%

## Acceptance Criteria
- [x] Disk 失败累计器(并发安全):实例级查询/写库失败 + 账号级适配器创建/枚举失败,归并 diskProviderFailure{provider, account_id, error_count, last_error} 随 Result.failures 携带
- [x] 探测不支持与真实无数据不计失败(三分语义)
- [x] 自我健康监控:必达厂商近 3 天零成功写库行 + 该厂商存在 ≥1 个 Disk 实例 → 升级告警(复用告警桥 AlertDiskZeroSuccess)
- [x] 仅全量运行判定,手动局部运行不误报
- [x] 单测:失败计数 + 路径三分 + 健康触发/抑制;go build ./... 通过

## Notes
coverage 91.3% 为本任务新增 disk_health_monitor.go checkMandatoryProviderHealth 的覆盖率(健康监控新增代码);变更涉及的 sync_disk_metrics.go 各函数 80.6~100%,executor 包整体 69.7%(含大量本期外文件)。golangci-lint 未安装按 Makefile 分支跳过;gofmt 仅对变更文件执行(避免全仓重排波及范围外文件)。

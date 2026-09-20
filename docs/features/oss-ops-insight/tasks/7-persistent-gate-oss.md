---
id: "7"
title: "scheduler_state 日闸 oss 键接入 + 采集任务注册"
priority: "P0"
estimated_time: "1h"
complexity: "low"
dependencies: [5]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 7: scheduler_state 日闸 oss 键接入 + 采集任务注册

## Description

给 NAS 已实现的持久化日闸加 `resource_type=oss` 分支,注册 `oss:collect_metrics` 每日提交;原子认领/写失败退避/读失败退避/特性开关回滚全部沿用,不改既有 nas/cdn 键行为。

## Reference Files

- `docs/proposals/oss-ops-insight/proposal.md` — Proposed Solution 第 4 条(持久化日闸复用)
- `internal/cam/scheduler/daily_gate.go`: 持久化日闸(资源类型分键/退避/告警)
- `internal/cam/scheduler/auto_sync_nas_metrics.go`: NAS 调度接入先例(分钟级循环认领→提交任务)
- `internal/cam/scheduler/feature_flag.go`: 特性开关 `SCHEDULER_PERSISTENT_GATE_ENABLED`
- `internal/cam/scheduler_gate_alerter.go`: 告警桥

## Acceptance Criteria

- [ ] 日闸支持 `resource_type=oss`:认领条件 `resource_type=oss AND last_date<today`,首次无记录视为首次认领(触发一次当日提交,不回溯补采)
- [ ] 分钟级循环调用 oss 日闸,认领成功才提交 `oss:collect_metrics`(days=2 语义);原子认领为唯一提交入口
- [ ] 写失败指数退避重试+升级告警;读失败 ≥5 分钟退避(沿用既有实现,验证 oss 键路径)
- [ ] 特性开关回滚:切回内存闸后 NAS/CDN/OSS 调度仍可提交、采集不中断
- [ ] 单测:oss 键原子认领并发(多 goroutine 只一胜)、首次认领过渡、退避/告警、开关回滚后调度可用;既有 nas/cdn 键测试不回归
- [ ] `go build ./...` 通过

## Hard Rules

- 按 resource_type 分键(nas/cdn/oss 独立),互不覆盖;不得改动既有 nas/cdn 键行为
- 原子认领是唯一提交入口

## Implementation Notes

- T7(NAS 日闸)已实现通用持久化日闸 + 原子认领,OSS 只需加 `resource_type=oss` 分支 + 调度接入文件(仿 auto_sync_nas_metrics.go 新建 auto_sync_oss_metrics.go)。
- 特性开关回滚逻辑(NAS/CDN 内存闸回退路径)已存在,OSS 平移。
- 单测覆盖 oss 键并发认领与首次过渡;回归验证既有 nas/cdn 日闸测试全绿。

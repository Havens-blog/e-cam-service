---
id: "7"
title: "持久化日闸 + 原子认领(scheduler_state, NAS 采用)"
priority: "P0"
estimated_time: "2d"
complexity: "high"
dependencies: [5]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 7: 持久化日闸 + 原子认领(scheduler_state, NAS 采用)

## Description

实现 `scheduler_state` 持久化日闸供 NAS 每日采集调度使用:findOneAndUpdate 原子认领当日 + 写失败指数退避重试/升级告警 + 读失败 ≥5 分钟退避。替换 CDN 内存闸的迁移部分在 T8。

## Reference Files
- `docs/proposals/nas-ops-insight/proposal.md` — Proposed Solution(持久化日闸 + 原子认领)、日快照取值口径与采集窗口、Key Risks、Success Criteria
- `internal/cam/scheduler/auto_sync.go`: AutoSyncScheduler + 内存闸(29-31 行,111 行 checkMetricsCollection)
- `internal/cam/scheduler/auto_sync_metrics.go`: checkMetricsCollection 每日触发逻辑
- `internal/cam/wire.go`: scheduler 装配(129 行 NewAutoSyncScheduler)
- `pkg/mongox/`: MongoDB 访问封装

## Acceptance Criteria
- [ ] `scheduler_state` collection:按 `resource_type`(nas/cdn 独立)记录 `last_date`;首次无记录视为首次认领、认领后触发一次当日提交(不回溯补采历史)
- [ ] 原子认领:一次 `findOneAndUpdate`(条件 `resource_type=nas AND last_date < today`,更新为 today),认领成功才提交采集任务;多副本/手动+自动重叠时同一资源只被一个实例认领
- [ ] 写失败降级:日闸写入失败走指数退避重试并升级告警(非仅记日志);读失败设置 ≥5 分钟退避窗口再重读,防分钟级循环洪泛任务队列
- [ ] NAS 每日采集接入:调度器分钟级循环调用持久化日闸(取代内存闸),认领成功提交 `nas:collect_metrics` 任务(days=2 语义)
- [ ] 单测:原子认领并发(多 goroutine 同触发只一个成功)、写失败退避+告警、读失败 ≥5 分钟退避、首次认领过渡行为
- [ ] `go build ./...` 通过

## Hard Rules
- 写失败必须重试+告警,不得仅记日志(否则「写失败+重启」让重启重复提交缺陷回归)
- 原子认领是唯一提交入口(认领成功才提交任务)
- 按 resource_type 分键(nas/cdn 独立),互不覆盖

## Implementation Notes
- `scheduler_state` 文档结构建议:`{resource_type, last_date, updated_at}`;findOneAndUpdate 用 `$lt` 条件 + `$set` 更新 + upsert。
- 本任务先做 NAS 键;CDN 迁移(改 CDN 键 + 值回归 + 特性开关回滚)在 T8。
- 给 scheduler 注入 scheduler_state 访问能力(wire.go 装配 DAO 或直接 mongo)。
- 风险提示(proposal):持久化日闸引入新 mongo 读写失败点 → 原子认领 + 退避 + T8 回滚开关三层兜底。

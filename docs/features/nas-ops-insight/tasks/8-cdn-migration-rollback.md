---
id: "8"
title: "CDN 日闸迁移到持久化 + 值回归 + 特性开关回滚"
priority: "P1"
estimated_time: "1.5d"
complexity: "medium"
dependencies: [7]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 8: CDN 日闸迁移到持久化 + 值回归 + 特性开关回滚

## Description

把 CDN 现有内存 `lastMetricsCollectDate` 迁移到 T7 的持久化日闸(cdn 键),补 CDN 迁移值回归(对比迁移前后同域同日值无缺口、无重复历史),加特性开关回滚(切回内存闸调度仍可用)。

## Reference Files
- `docs/proposals/nas-ops-insight/proposal.md` — Proposed Solution(持久化日闸 + 原子认领:CDN 迁移回归与首部署过渡、特性开关与回滚)、Success Criteria
- `internal/cam/scheduler/auto_sync.go`: 内存 `lastMetricsCollectDate`(29-31 行)
- `internal/cam/scheduler/auto_sync_metrics.go`: CDN `checkMetricsCollection`(37-76 行,内存闸读取)
- `internal/cam/scheduler/`: T7 持久化日闸(本任务迁移 CDN 到其 cdn 键)

## Acceptance Criteria
- [ ] CDN `checkMetricsCollection` 从读内存 `lastMetricsCollectDate` 改为读持久化日闸(cdn 键);`auto_sync.go` 内存字段移除或保留兼容
- [ ] CDN 迁移值回归:对比迁移前后同域同日值无缺口、无重复历史(仍按 days=2 语义正确写入);首部署 scheduler_state 无 cdn 记录 → 按「首次认领 → 触发一次当日提交」过渡行为执行
- [ ] 特性开关回滚:`SCHEDULER_PERSISTENT_GATE_ENABLED` 默认开启;开关切回内存闸后 NAS/CDN 调度任务仍正常提交、指标采集不中断
- [ ] 连续 3 次重启 × 每次观察 CDN 任务各仅 1 条;写失败注入验证退避+告警;读失败注入验证 ≥5 分钟退避内不重复提交
- [ ] 单测:迁移值回归、首部署过渡、开关回滚后调度可用;`go build ./...` 通过

## Hard Rules
- 迁移后 CDN 不得重复提交(重启×3 各仅 1 条)也不得丢历史
- 回滚开关是硬要求(生产行为变更必须有退路)

## Implementation Notes
- 迁移基准:以迁移前内存闸期间已落库的 CDN 日值为基线,迁移后对比无缺口。
- 特性开关经配置读取(参照现有 config/env 注入模式);回滚验证纳入 SC。
- 与 T6 的健康监控共用同一告警通道(勿重复造)。
- 风险提示(proposal):「写失败+重启」会让重启重复提交缺陷回归 → 写失败必须重试+告警(T7 已实现,本任务验证 CDN 场景)。

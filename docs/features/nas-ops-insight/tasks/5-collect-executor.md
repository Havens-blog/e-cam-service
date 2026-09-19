---
id: "5"
title: "nas:collect_metrics 采集执行器(账号遍历 + 首写生效 + 注册)"
priority: "P0"
estimated_time: "2d"
complexity: "high"
dependencies: [3, 4]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 5: nas:collect_metrics 采集执行器(账号遍历 + 首写生效 + 注册)

## Description

实现 `nas:collect_metrics` 执行器:按活跃账号遍历 NAS 实例 → 调 NASMetricQuerier → 写入 `ecam_nas_metric`。落实「首写生效(仅保护今日行)+ 次日补昨日覆盖更新」的 upsert 语义,注册到 task 模块。

## Reference Files
- `docs/proposals/nas-ops-insight/proposal.md` — Proposed Solution、日快照取值口径与采集窗口、Requirements Analysis、Key Risks、Success Criteria
- `internal/cam/task/executor/sync_cdn_metrics.go`: CDN 指标执行器参照(账号互斥/进度/Result)
- `internal/cam/task/module.go`: 执行器注册参照(52-56 行)
- `internal/cam/scheduler/auto_sync_metrics.go`: 每日触发参照(调度挂载点)
- `internal/shared/domain/asset_types.go`: 资产类型清单(已含 nas)

## Acceptance Criteria
- [ ] 执行器按活跃账号(租户下已纳管且存在 ≥1 个 NAS 实例的云账号,以 ecam_instance 枚举为准)遍历 NAS 实例,不依赖账号 EnableAutoSync 开关
- [ ] 每日采集区间 `[昨日, 今日]`:补昨日完整行(覆盖更新昨日行)+ 今日初态(首写生效仅保护今日行);同日重复采集不产生脏行
- [ ] 不继承 CDN 全零跳过过滤:`capacity=0` 异常行落库可见(带 qc_status=zero_exception)
- [ ] 注册到 `internal/cam/task/module.go`(`nas:collect_metrics` 类型),执行器通过账号级互斥避免同账号并发踩踏
- [ ] 单测:活跃账号筛选、首写生效 vs 昨日覆盖、零容量行落库可见、账号互斥
- [ ] `go build ./...` 通过

## Hard Rules
- 同日首写生效仅保护今日行;昨日行由次日补采显式覆盖更新(不得被首写挡住)
- 活跃账号口径 = 已纳管且存在 ≥1 个 NAS 实例(不以 EnableAutoSync 为门槛)
- 不继承 CDN「当日全零即跳过」过滤

## Implementation Notes
- 参照 `internal/cam/task/executor/sync_cdn_metrics.go` 的 CDN 执行器结构(账号级互斥 `tryAcquireAccount`/`releaseAccount`、进度更新、Result 汇总)。
- 首写生效 upsert:DAO 层用「filter 含当日行不存在才插入」或 `$setOnInsert` 保护今日行;昨日覆盖用普通 upsert(T2 DAO 已定唯一键)。
- 失败可观测(失败计数+末次错误入 Result)与自我健康监控(连续 3 天零成功告警)在 T6 补,本任务先保证执行器主体正确。
- 每日调度挂载在 T7(持久化日闸)统一接入;本任务仅实现执行器本体 + 注册。

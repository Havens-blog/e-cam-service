---
id: "6"
title: "采集失败可观测 + 自我健康监控(失败计数入 Result + 连续零成功告警)"
priority: "P1"
estimated_time: "1.5d"
complexity: "medium"
dependencies: [5]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 6: 采集失败可观测 + 自我健康监控(失败计数入 Result + 连续零成功告警)

## Description

为 NAS 指标采集补上「失败可观测」:执行器按厂商/账号维护失败计数与末次错误、任务 Result 携带;适配器区分「探测不支持」(INFO)与「调用失败返回空」(ERROR);自我健康监控——必达厂商连续 3 天零成功采集且该厂商有 ≥1 个 NAS 实例时升级告警。

## Reference Files
- `docs/proposals/nas-ops-insight/proposal.md` — 失败可观测性、Requirements Analysis、Key Risks、Success Criteria
- `internal/cam/task/executor/sync_cdn_metrics.go`: CDN `skipped_providers` 雏形参照(失败汇总)
- `internal/cam/task/executor/`: T5 采集执行器(本任务扩展其 Result 结构)
- `docs/features/nas-ops-insight/tasks/5-collect-executor.md`: 执行器本体(本任务依赖)

## Acceptance Criteria
- [ ] 执行器按厂商/账号维护失败计数与末次错误,任务 Result 携带(扩展 CDN `skipped_providers` 雏形为含错误明细的结构,运营可查)
- [ ] 适配器失败路径可分辨:探测不支持 → INFO;调用失败返回空 → ERROR + error 字段(不静默)
- [ ] 自我健康监控:必达厂商(以 T1 分组为准)连续 3 天零成功采集、且该厂商实盘存在 ≥1 个 NAS 实例 → 升级告警(与日闸告警通道共用);无 NAS 实例厂商不触发
- [ ] 单测:失败计数写入 Result、探测不支持 vs 调用失败的日志分级、连续零成功告警触发/抑制条件
- [ ] `go build ./...` 通过

## Hard Rules
- 失败必须可观测:不得让「适配器失效」与「真实无指标」在结果上不可分辨
- 自我健康监控必须带「该厂商存在 ≥1 个 NAS 实例」前置(防无实例厂商误报)

## Implementation Notes
- 参照 CDN 执行器 `skipped_providers`(sync_cdn_metrics.go)扩展为含 `provider/account_id/error_count/last_error` 的结构。
- 健康监控可挂在每日采集完成钩子:检查各必达厂商近 3 天是否有成功写库行(查 ecam_nas_metric),全零且实例存在 → 告警。
- 告警通道与 T8 CDN 迁移的日闸告警共用(同一定义,勿重复造)。
- 风险提示(proposal):前端空态区分「无数据」与「采集失败/未启用」依赖本任务的 Result 失败计数——前端消费在 T11。

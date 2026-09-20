---
id: "6"
title: "采集失败可观测 + 自我健康监控(失败计数入 Result + 连续零成功告警)"
priority: "P1"
estimated_time: "1.5h"
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

为 OSS 采集加失败可观测:失败计数归并入 `Result["failures"]`,必达厂商(aliyun/huawei/aws)近 3 天零成功写库 + 存在 ≥1 个 OSS bucket → 升级告警(复用告警桥)。

## Reference Files

- `docs/proposals/oss-ops-insight/proposal.md` — Non-Functional Requirements(可观测性/健康告警)、Proposed Solution 第 3 条
- `internal/cam/task/executor/nas_health_monitor.go`: NAS 自我健康监控先例(必达厂商零成功判定)
- `internal/cam/task/executor/sync_nas_metrics.go`: NAS 失败累计器先例(nasAccountFailure/nasProviderFailure)
- `internal/cam/scheduler_gate_alerter.go`: 告警桥(SchedulerGateAlerter/AlertNASZeroSuccess)

## Acceptance Criteria

- [ ] OSS 失败累计器(并发安全):实例级查询/写库失败 + 账号级适配器创建/枚举失败,归并 `ossProviderFailure{provider, account_id, error_count, last_error}` 随 Result.failures 携带
- [ ] 探测不支持与真实无数据不计失败(三分语义)
- [ ] 自我健康监控:必达厂商近 3 天零成功写库行 + 该厂商存在 ≥1 个 OSS bucket → 升级告警(复用告警桥 AlertOSSZeroSuccess 或泛化 NAS 版)
- [ ] 仅全量运行判定,手动局部运行不误报
- [ ] 单测:失败计数 4 + 路径三分 + 健康触发/抑制;`go build ./...` 通过

## Hard Rules

- 复用既有告警桥(SchedulerGateAlerter),不得重复造告警通道
- 探测不支持/真实无数据不得计为失败

## Implementation Notes

- NAS 健康监控(nas_health_monitor.go)是直接蓝本,OSS 平移或泛化共用。
- 告警桥如已有 NAS 版,OSS 复用同一实例加 `AlertOSSZeroSuccess` 方法(参照 T6 的 AlertNASZeroSuccess 先例)。
- 必达厂商清单与 T1 报告一致。

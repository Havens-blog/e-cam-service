---
id: "5"
title: "disk:collect_metrics 采集执行器(账号遍历 + 首写生效 + 注册)"
priority: "P0"
estimated_time: "1.5h"
complexity: "medium"
dependencies: [2, 3, 4]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 5: disk:collect_metrics 采集执行器(账号遍历 + 首写生效 + 注册)

## Description

实现 Disk 指标采集执行器 `disk:collect_metrics`:按活跃账号(≥1 个 Disk 实例)遍历磁盘 → 调 DiskMetricQuerier → 写 ecam_disk_metric;今日行首写生效,昨日行覆盖;账号互斥 + disk 有界并发;注册到 module.go。

## Reference Files

- `docs/proposals/disk-ops-insight/proposal.md` — Proposed Solution 第 3 条(活跃账号口径/首写生效/并发)
- `internal/cam/task/executor/sync_nas_metrics.go`: NAS 采集执行器先例(账号遍历/首写生效/失败计数/nasAccountGate 互斥)
- `internal/cam/task/executor/sync_oss_metrics.go`: OSS 采集执行器先例(最近平移产物,直接蓝本)
- `internal/cam/task/executor/nas_account_gate.go`: 共享账号互斥闸
- `internal/cam/task/module.go`: 任务注册

## Acceptance Criteria

- [ ] `disk:collect_metrics` 执行器:按活跃账号(ecam_instance 枚举 ≥1 个 Disk 实例,不依赖 EnableAutoSync)遍历 → DiskMetricQuerier → 写 ecam_disk_metric
- [ ] 今日行首写生效(复用 DAO BulkInsertIfAbsent,命中已有行不修改),昨日行覆盖更新;不继承 CDN 全零过滤(usage_percent=0 落库打 zero_exception)
- [ ] 账号级互斥 + disk 有界并发 5(复用/泛化 nasAccountGate)
- [ ] 失败计数入 `Result["failures"]`(provider/account/error_count/last_error);探测不支持与真实无数据不计失败
- [ ] 已注册 module.go;`disk:collect_metrics` 任务可被调度器提交
- [ ] 单测:账号遍历/首写生效/失败计数/并发互斥;`go build ./...` 通过

## Hard Rules

- 活跃账号口径 = 租户下已纳管且存在 ≥1 个 Disk 实例的云账号,不得用 EnableAutoSync 过滤
- 复用共享账号互斥闸,不得重造

## Implementation Notes

- NAS/OSS 执行器是直接蓝本(sync_nas_metrics.go / sync_oss_metrics.go),Disk 第三次平移;复用 nasAccountGate(或泛化为 assetAccountGate)。
- DAO BulkInsertIfAbsent 已实现(T2),执行器直接调用。
- 采集任务注册后由 T7 日闸 disk 键每日提交。
- 注意:available(未挂载)磁盘的 usage_percent 可能无意义——按 T1 探测结论处理(空或打标)。

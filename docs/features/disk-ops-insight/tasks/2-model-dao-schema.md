---
id: "2"
title: "DiskMetric 模型 + DiskMetricQuerier 接口 + ecam_disk_metric DAO 建表"
priority: "P0"
estimated_time: "1.5h"
complexity: "medium"
dependencies: [1]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 2: DiskMetric 模型 + DiskMetricQuerier 接口 + ecam_disk_metric DAO 建表

## Description

建 Disk 指标模型与 DAO:DiskMetric(disk_id/date/usage_percent/iops/throughput/qc_status)+ DiskMetricQuerier 可选接口(带 region——云硬盘是地域性资源,与 NAS 同型)+ ecam_disk_metric DAO(唯一键 `(account_id, disk_id, date)` + 数量级门禁)。

## Reference Files

- `docs/proposals/disk-ops-insight/proposal.md` — Proposed Solution 第 1/2/5 条(模型/唯一键/读取契约)、Non-Functional Requirements(单位归一化)
- `internal/shared/cloudx/types/nas_metric.go`: NASMetric 模型先例(qc_status 常量/单位归一化)
- `internal/shared/cloudx/interfaces.go`: NASMetricQuerier 接口先例(带 region 版,Disk 平移)
- `internal/cam/repository/dao/nas_metric.go`: NAS DAO 先例(唯一键/首写生效/门禁/零值放行)
- `internal/shared/cloudx/types/disk.go`: DiskInstance 结构(Size/IOPS/Throughput 字段语义)

## Acceptance Criteria

- [ ] DiskMetric 模型落库字段:disk_id/date/usage_percent(float64,百分比 0~100)/iops(float64)/throughput(float64)/qc_status(空=正常,zero_exception=使用率 0 异常行);时延不落库(二期)
- [ ] DiskMetricQuerier 可选接口:`GetDiskMetrics(ctx, diskID, diskName, region, startDate, endDate) ([]types.DiskMetric, error)`,签名带 region(地域性资源);在 interfaces.go 注册
- [ ] ecam_disk_metric DAO:唯一索引 `(account_id, disk_id, date)`(跨账号共享磁盘防互相覆盖);UpsertMetric/BulkInsertIfAbsent 首写生效语义(今日行不覆盖,昨日行补采覆盖)
- [ ] 写入门禁数量级:usage_percent 限 0~100 越界拒绝,zero_exception 放行打标;iops/throughput 非负校验;容量字节 → GB 走共享 `types.BytesToGB`(如适用)
- [ ] 单测:模型序列化、DAO 唯一键隔离(多账号同 disk_id 同日各留一行)/幂等/门禁(usage_percent 越界拒绝+零值放行);live 测试 MONGO_DSN 门控
- [ ] 装配位(module.go/wire.go)留待 T5/T7 接线(不提前接)

## Hard Rules

- 唯一键必须含 account_id——跨账号共享磁盘不得互相覆盖(3cea6d7 多账号修复经验)
- 不落库时延、不落库派生 utilization(如适用时读取派生)

## Implementation Notes

- 复用 NAS DAO 的 BulkInsertIfAbsent(首写生效)模式;门禁常量与 NAS/OSS 共用或同值。
- 云硬盘是地域性资源:Querier 带 region 参数(与 NAS 同型,非 OSS);唯一键用 disk_id(地域内唯一)类比 NAS fs_id。
- qc_status 读取侧闭环留到 T8 读取接口,本期只做写路径。
- usage_percent 口径以 T1 探测归一结果为准(云盘级 vs 派生)。

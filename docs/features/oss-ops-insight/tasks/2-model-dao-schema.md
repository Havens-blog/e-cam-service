---
id: "2"
title: "OSSMetric 模型 + OSSMetricQuerier 接口 + ecam_oss_metric DAO 建表"
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

# 2: OSSMetric 模型 + OSSMetricQuerier 接口 + ecam_oss_metric DAO 建表

## Description

建 OSS 容量指标模型与 DAO:OSSMetric(bucket_name/date/storage_size/object_count/qc_status)+ OSSMetricQuerier 可选接口(无 region——OSS 是全局服务,与 CDN 同型)+ ecam_oss_metric DAO(唯一键 `(account_id, bucket_name, date)` + 写入门禁)。

## Reference Files

- `docs/proposals/oss-ops-insight/proposal.md` — Proposed Solution 第 1/2/5 条(模型/唯一键/读取契约)
- `internal/shared/cloudx/types/nas_metric.go`: NASMetric 模型先例(qc_status 常量/单位归一化)
- `internal/shared/cloudx/interfaces.go`: NASMetricQuerier 接口先例(带 region 版,OSS 去掉 region)
- `internal/cam/repository/dao/nas_metric.go`: NAS DAO 先例(唯一键/首写生效/门禁/零值放行)
- `internal/shared/cloudx/types/oss.go`: OSSBucket 结构(StorageSize/ObjectCount 字段语义)

## Acceptance Criteria

- [ ] OSSMetric 模型落库字段:bucket_name/date/storage_size(GB float64)/object_count(int64)/qc_status(空=正常,zero_exception=容量 0 异常行);分层大小不落库
- [ ] OSSMetricQuerier 可选接口:`GetOSSMetrics(ctx, bucketName, startDate, endDate) ([]types.OSSMetric, error)`,签名无 region(OSS 全局服务);在 interfaces.go 注册
- [ ] ecam_oss_metric DAO:唯一索引 `(account_id, bucket_name, date)`(多账号同 bucket 名并存防互相覆盖);UpsertMetric/BulkInsertIfAbsent 首写生效语义(今日行不覆盖,昨日行补采覆盖)
- [ ] 写入门禁 [1MB, 1PB] 数量级:零值放行打 zero_exception、非零越界拒绝;单位字节→GB 走共享 `types.BytesToGB`
- [ ] 单测:模型序列化、DAO 唯一键隔离(三账号同 bucket 名同日各留一行)/幂等/门禁(零值放行+越界拒绝);live 测试 MONGO_DSN 门控
- [ ] 装配位(module.go/wire.go)留待 T5/T7 接线(不提前接)

## Hard Rules

- 唯一键必须含 account_id——跨账号共享 bucket 名不得互相覆盖(3cea6d7 多账号修复经验)
- 不落库分层大小、不落库 utilization(读取时派生)

## Implementation Notes

- 复用 NAS DAO 的 BulkInsertIfAbsent(首写生效)模式;`[1MB,1PB]` 门禁常量与 NAS 共用或同值。
- OSS 是全局服务:Querier 无 region 参数,与 CDN 同型;唯一键用 bucket_name(全局唯一)类比 CDN domain。
- qc_status 读取侧闭环留到 T10 读取接口,本期只做写路径。

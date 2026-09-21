---
id: "2"
title: "RDSMetric 模型 + RDSMetricQuerier 接口 + ecam_rds_metric DAO 建表"
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

# 2: RDSMetric 模型 + RDSMetricQuerier 接口 + ecam_rds_metric DAO 建表

## Description

建 RDS 指标模型与 DAO:RDSMetric(rds_id/date/cpu_percent/memory_percent/disk_percent/connections/qc_status)+ RDSMetricQuerier 可选接口(带 region——云数据库是地域性资源,与 NAS/Disk 同型)+ ecam_rds_metric DAO(唯一键 `(account_id, rds_id, date)` + 0~100 门禁)。

## Reference Files

- `docs/proposals/rds-ops-insight/proposal.md` — Proposed Solution 第 1/2/5 条(模型/唯一键/读取契约)、Non-Functional Requirements(单位归一化)
- `internal/shared/cloudx/types/disk_metric.go`: DiskMetric 模型先例(qc_status/usage_scope 常量,Disk 平移产物)
- `internal/shared/cloudx/interfaces.go`: DiskMetricQuerier 接口先例(带 region 版,RDS 平移)
- `internal/cam/repository/dao/disk_metric.go`: Disk DAO 先例(唯一键/首写生效/门禁/零值放行)
- `internal/shared/cloudx/types/rds.go`: RDSInstance 结构(Engine/CPU/Memory/Storage 字段语义)

## Acceptance Criteria

- [ ] RDSMetric 模型落库字段:rds_id/date/cpu_percent(float64 0~100)/memory_percent(float64 0~100)/disk_percent(float64 0~100)/connections(int64)/qc_status(空=正常,zero_exception=全 0 异常行);engine 作为元数据(如需要可附字段)
- [ ] RDSMetricQuerier 可选接口:`GetRDSMetrics(ctx, rdsID, instanceName, region, engine, startDate, endDate) ([]types.RDSMetric, error)`,签名带 region + engine(地域性资源 + 多引擎);在 interfaces.go 注册
- [ ] ecam_rds_metric DAO:唯一索引 `(account_id, rds_id, date)`(跨账号共享实例防互相覆盖);UpsertMetric/BulkInsertIfAbsent 首写生效语义(今日行不覆盖,昨日行补采覆盖)
- [ ] 写入门禁:三使用率限 0~100 越界拒绝,zero_exception 放行打标;connections 非负校验
- [ ] 单测:模型序列化、DAO 唯一键隔离(多账号同 rds_id 同日各留一行)/幂等/门禁(使用率越界拒绝+零值放行);live 测试 MONGO_DSN 门控
- [ ] 装配位(module.go/wire.go)留待 T5/T7 接线(不提前接)

## Hard Rules

- 唯一键必须含 account_id——跨账号共享实例不得互相覆盖(3cea6d7 多账号修复经验)
- 不落库 QPS/IOPS 吞吐、不落库派生值(如适用时读取派生)

## Implementation Notes

- 复用 Disk DAO 的 BulkInsertIfAbsent(首写生效)模式;门禁常量与 NAS/OSS/Disk 共用或同值。
- 云数据库是地域性资源:Querier 带 region 参数(与 NAS/Disk 同型);唯一键用 rds_id(地域内唯一)类比 Disk disk_id。
- engine 透传:适配器采集时不因 engine 分支,engine 作为元数据附在模型或留待前端展示(探测确认是否需要独立字段)。
- qc_status 读取侧闭环留到 T8 读取接口,本期只做写路径。
- 内存/CPU/磁盘使用率口径以 T1 探测归一结果为准(百分比 0~100)。

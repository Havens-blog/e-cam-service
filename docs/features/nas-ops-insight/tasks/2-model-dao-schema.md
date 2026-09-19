---
id: "2"
title: "NASMetric 模型 + NASMetricQuerier 接口 + ecam_nas_metric DAO 建表"
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

# 2: NASMetric 模型 + NASMetricQuerier 接口 + ecam_nas_metric DAO 建表

## Description

冻结 NAS 指标 schema:定义 `types.NASMetric` 模型、`cloudx.NASMetricQuerier` 可选接口(带 region,区别于 CDN 全局签名),以及 `ecam_nas_metric` 表的 DAO(唯一键 `(account_id, fs_id, date)`)。

## Reference Files
- `docs/proposals/nas-ops-insight/proposal.md` — Proposed Solution、单位归一化与字段语义、日快照取值口径与采集窗口、聚合口径、Non-Functional Requirements、Success Criteria
- `internal/shared/cloudx/types/cdn.go`: CDNMetric 模型参照(92-100 行)
- `internal/shared/cloudx/interfaces.go`: CDNMetricQuerier 参照(626-635 行)
- `internal/cam/repository/dao/cdn_metric.go`: CDNMetricDAO 参照(唯一键 upsert 模式)
- `internal/shared/cloudx/types/nas.go`: NASInstance 现有模型(容量字段语义参照)

## Acceptance Criteria
- [ ] `types.NASMetric` 落库字段:`fs_id / date / capacity(GB) / used_capacity(GB) / qc_status`;utilization 不落库(读取时由 capacity/used 派生);qc_status 含 `zero_exception` 取值
- [ ] `cloudx.NASMetricQuerier` 接口:`GetNASMetrics(ctx, fsID, fsName, region, startDate, endDate) ([]types.NASMetric, error)` — 签名带 region(CDN 全局签名无 region,NAS 是地域性资源)
- [ ] `ecam_nas_metric` 唯一索引 `(account_id, fs_id, date)`(Unique);DAO 提供 UpsertMetric(首写生效+昨日覆盖语义见 T4 执行器,DAO 层先保证键唯一)
- [ ] 数量级自检:写入前 capacity 落在 [1MB, 1PB] 区间校验;capacity=0 例外放行并打 `qc_status=zero_exception` 标(不拦截不跳过)
- [ ] 单测:DAO upsert 同账号同日幂等、跨账号同 fs 并存各一行(仿 CDN `TestCDNMetricPerAccountLive`)

## Hard Rules
- capacity 单位必须 GB(二进制 GiB),禁止字节直接写 GB 字段
- utilization 不落库(避免重采三字段不一致)
- 唯一键必须含 account_id(多账号同 fs 并存,复用 CDN 修复经验)

## Implementation Notes
- 参照 `internal/cam/repository/dao/cdn_metric.go` 的 CDNMetricDAO 实现(索引创建 + 参数边界收敛 + live 测试模式)。
- 参照 `internal/cam/task/module.go:55` 与 `internal/cam/wire.go:135` 的 NewCDNMetricDAO 装配点,为 NAS DAO 预留对应装配位(实际接线在 T4/T7)。
- 风险提示(proposal 聚合口径):唯一键是 `(account_id, fs_id, date)`;运营卡/Top 的 fs_id 去重是读侧逻辑,不在本任务。
- 测试参照 `internal/cam/repository/dao/cdn_metric_live_test.go` 的 `TestCDNMetricPerAccountLive`(MONGO_DSN 门控)。

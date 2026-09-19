---
id: "10"
title: "NAS 指标读取接口:单实例趋势 + Top(租户校验 + 分页 + qc_status 闭环)"
priority: "P1"
estimated_time: "2d"
complexity: "medium"
dependencies: [2, 7]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 10: NAS 指标读取接口:单实例趋势 + Top(租户校验 + 分页 + qc_status 闭环)

## Description

实现 `GET /assets/nas/metrics`(单实例趋势)与 `GET /assets/nas/top`(账号视角 Top)读取接口。服务端校验 account_id ∈ 租户账号集合(防越权),Top 按 fs_id 去重聚合(日期 desc 再容量 desc),趋势与 Top 返回最新一天 + 近 N 天均值,响应暴露 qc_status/data_status。

## Reference Files
- `docs/proposals/nas-ops-insight/proposal.md` — Proposed Solution(读取接口契约)、聚合口径、Non-Functional Requirements、Success Criteria
- `internal/cam/service/asset_cdn_query.go`: CDN 读取服务参照(tenantAccountIDs 逐账号隔离模式)
- `internal/cam/web/asset_handler_storage.go`: NAS handler 参照(ListNAS/GetNAS)
- `internal/cam/repository/dao/cdn_metric.go`: CDN Metric DAO 读取参照(ListByDomain/TopByBytes)
- `internal/cam/repository/dao/`: 本 feature T2 的 NAS Metric DAO

## Acceptance Criteria
- [ ] `GET /assets/nas/metrics?fs_id=&account_id=&days=`:返回 `{fs_id, days[]}`;days[] 按日期升序,每项 `date/capacity/used/utilization/data_status/qc_status`;缺失日以 data_status 标注不填充假值;days 限 1~90;趋势同时返回「最新一天」与「近 N 天均值」两类值,utilization 由 capacity/used 读取时派生
- [ ] `GET /assets/nas/top?account_id=&days=&sort=&top=&page=&page_size=`:sort ∈ capacity|utilization(utilization 用近 N 天均值);top=N(默认 10 最大 50);分页 page(默认 1)/page_size(默认 10 最大 50);响应 `{total, page, page_size, items[]}`(Top 亦返回最新一天 + 近 N 天均值)
- [ ] Top 按 fs_id 去重:多账号并存按「日期 desc,再容量 desc」取第一行;平均使用率对无数据实例跳过(不记 0)、capacity=0 行不参与均值
- [ ] 租户校验:接口从鉴权上下文取 tenantID,校验 account_id ∈ 该租户账号集合;越权返回 404(不泄露账号存在性)
- [ ] qc_status 闭环:capacity=0(zero_exception)在读取响应中原样暴露并映射进 data_status;前端可分辨异常而非当正常零容量
- [ ] 单测:租户隔离(tenant A 查不到 B)、fs_id 去重、分页/limit、qc_status 传递、utilization 派生边界(capacity=0 → null);`go build ./...` 通过

## Hard Rules
- account_id 必须服务端校验归属租户,不得信任客户端参数
- Top 聚合必须按 fs_id 去重(不得跨账号求和/平均,避免共享容量双计)
- utilization 读取时派生,不读落库字段

## Implementation Notes
- 租户校验参照 `internal/cam/service/asset_cdn_query.go` 的 `tenantAccountIDs` + 逐账号过滤模式(CDN 已实现,平移)。
- Top 去重排序「日期 desc 再容量 desc」与 proposal「聚合口径」节一致;该行 capacity/used 作为物理 fs 口径,不做跨账号求和。
- handler 注册到 NAS 已有路由(参照 `asset_handler_storage.go`);service 层参照 `asset_cdn_query.go` 的 CDNQueryService 模式新增 NASQueryService。
- 风险提示(proposal):越权返回 404 而非 403(不泄露账号存在性)。
- 注意:Top 端点契约中「items[] 每条含 account_id 列表」——若某 fs 跨账号并存,items 聚合取代表行后 account_id 列表如何呈现需与 T11 前端确认口径。

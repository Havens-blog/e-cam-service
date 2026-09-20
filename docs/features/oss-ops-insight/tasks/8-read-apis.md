---
id: "8"
title: "OSS 指标读取接口:单 bucket 趋势 + Top(租户校验 + 分页 + qc_status 闭环)"
priority: "P1"
estimated_time: "2h"
complexity: "medium"
dependencies: [2, 7]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 8: OSS 指标读取接口:单 bucket 趋势 + Top(租户校验 + 分页 + qc_status 闭环)

## Description

实现 `GET /assets/oss/metrics`(单 bucket 趋势)与 `GET /assets/oss/top`(账号视角 Top)读取接口。服务端校验 account_id ∈ 租户账号集合(防越权),Top 按 bucket_name 去重聚合(日期 desc 再容量 desc),趋势与 Top 返回最新一天 + 近 N 天均值,响应暴露 qc_status/data_status。

## Reference Files

- `docs/proposals/oss-ops-insight/proposal.md` — Proposed Solution 第 5 条(读取接口契约)、Non-Functional Requirements(租户校验)
- `internal/cam/service/asset_nas_query.go`: NASQueryService 先例(租户隔离/fs_id 去重/派生 utilization)
- `internal/cam/web/asset_handler_nas_metrics.go`: NAS handler 先例(路由/days 校验/404 越权)
- `internal/cam/repository/dao/nas_metric_query.go`: NAS 读取 DAO 先例(ListByFs/ListByAccounts)
- `internal/cam/web/asset_handler.go`: OSS 既有路由(`/oss`、`/oss/:asset_id`)

## Acceptance Criteria

- [ ] `GET /assets/oss/metrics?bucket_name=&account_id=&days=`:返回 `{bucket_name, days[]}`;days[] 按日期升序,每项 `date/storage_size/object_count/data_status/qc_status`;缺失日以 data_status 标注不填假值;days 限 1~90;趋势同时返回「最新一天」与「近 N 天均值」两类值
- [ ] `GET /assets/oss/top?account_id=&days=&sort=&top=&page=&page_size=`:sort ∈ storage_size|object_count(用近 N 天均值);top 默认 10 最大 50;分页 page 默认 1/page_size 默认 10 最大 50;响应 `{total, page, page_size, items[]}`(含最新一天 + 近 N 天均值)
- [ ] Top 按 bucket_name 去重:多账号并存按「日期 desc,再容量 desc」取第一行;无数据实例跳过(不记 0)
- [ ] 租户校验:接口从鉴权上下文取 tenantID,校验 account_id ∈ 租户账号集合;越权返回 404(不泄露账号存在性)
- [ ] qc_status 闭环:storage_size=0(zero_exception)在读取响应中原样暴露并映射进 data_status;前端可分辨异常而非当正常空桶
- [ ] 单测:租户隔离(tenant A 查不到 B)、bucket_name 去重、分页/limit、qc_status 传递;`go build ./...` 通过

## Hard Rules

- account_id 必须服务端校验归属租户,不得信任客户端参数
- Top 聚合必须按 bucket_name 去重(不得跨账号求和/平均,避免共享存储双计)

## Implementation Notes

- NAS 读取服务/DAO/handler(asset_nas_query.go/nas_metric_query.go/asset_handler_nas_metrics.go)是直接蓝本,OSS 平移;路由注册到既有 `/oss` 组(`/oss/metrics`、`/oss/top` 与 `/oss/:asset_id` 共存,参照 NAS handler 注册冒烟测试)。
- Top items[].account_id 列表口径(bucket 跨账号并存时)与前端 T9 确认(参照 NAS T10 记录的对齐项)。
- 与既有 ListOSS/GetOSS handler 共存时注意 gin 路由冲突(带冒烟测试防 panic)。

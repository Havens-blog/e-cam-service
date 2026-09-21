---
id: "8"
title: "RDS 指标读取接口:单实例趋势 + Top(租户校验 + 分页 + qc_status 闭环)"
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

# 8: RDS 指标读取接口:单实例趋势 + Top(租户校验 + 分页 + qc_status 闭环)

## Description

实现 `GET /assets/rds/metrics`(单实例趋势)与 `GET /assets/rds/top`(账号视角 Top)读取接口。服务端校验 account_id ∈ 租户账号集合(防越权),Top 按 rds_id 去重聚合(日期 desc 再 CPU desc),趋势与 Top 返回最新一天 + 近 N 天均值,响应暴露 qc_status/data_status。

## Reference Files

- `docs/proposals/rds-ops-insight/proposal.md` — Proposed Solution 第 5 条(读取接口契约)、Non-Functional Requirements(租户校验)
- `internal/cam/service/asset_disk_query.go`: DiskQueryService 先例(最近平移产物,直接蓝本)
- `internal/cam/web/asset_handler_disk_metrics.go`: Disk handler 先例(路由/days 校验/404 越权)
- `internal/cam/web/asset_handler_database.go`: RDS 既有路由(`/rds`、`/rds/:asset_id`)

## Acceptance Criteria

- [ ] `GET /assets/rds/metrics?rds_id=&account_id=&days=`:返回 `{rds_id, days[]}`;days[] 按日期升序,每项 `date/cpu_percent/memory_percent/disk_percent/connections/data_status/qc_status`;缺失日以 data_status 标注不填假值;days 限 1~90;趋势同时返回「最新一天」与「近 N 天均值」两类值
- [ ] `GET /assets/rds/top?account_id=&days=&sort=&top=&page=&page_size=`:sort ∈ cpu_percent|memory_percent|disk_percent|connections(用近 N 天均值);top 默认 10 最大 50;分页 page 默认 1/page_size 默认 10 最大 50;响应 `{total, page, page_size, items[]}`(含最新一天 + 近 N 天均值)
- [ ] Top 按 rds_id 去重:多账号并存按「日期 desc,再 CPU desc」取第一行;无数据实例跳过(不记 0)
- [ ] 租户校验:接口从鉴权上下文取 tenantID,校验 account_id ∈ 租户账号集合;越权返回 404(不泄露账号存在性)
- [ ] qc_status 闭环:四指标全 0(zero_exception)在读取响应中原样暴露并映射进 data_status;前端可分辨异常而非当正常空负载;停用/重启中实例打标 data_status 而非 zero_exception
- [ ] 单测:租户隔离(tenant A 查不到 B)、rds_id 去重、分页/limit、qc_status 传递;`go build ./...` 通过

## Hard Rules

- account_id 必须服务端校验归属租户,不得信任客户端参数
- Top 聚合必须按 rds_id 去重(不得跨账号求和/平均,避免共享实例双计)

## Implementation Notes

- Disk 读取服务/DAO/handler 是直接蓝本,RDS 平移;路由注册到既有 `/rds` 组(`/rds/metrics`、`/rds/top` 与 `/rds/:asset_id` 共存,参照 Disk handler 注册冒烟测试)。
- Top items[].account_id 列表口径(实例跨账号并存时)与前端 T9 确认(参照 NAS T10 记录的对齐项)。
- 与既有 ListRDS/GetRDS handler 共存时注意 gin 路由冲突(带冒烟测试防 panic)。

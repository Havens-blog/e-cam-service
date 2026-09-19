---
status: "completed"
started: "2026-09-19 18:38"
completed: "2026-09-19 19:04"
time_spent: "~26m"
---

# Task Record: 10 NAS 指标读取接口:单实例趋势 + Top(租户校验 + 分页 + qc_status 闭环)

## Summary
实现 NAS 指标读取接口:GET /assets/nas/metrics(单实例趋势,窗口逐日升序展开、缺失日 data_status=missing 不填假值、最新一天+近 N 天均值、utilization 读取时派生)与 GET /assets/nas/top(账号视角 Top,按 fs_id 去重取「日期 desc 再容量 desc」代表行、不跨账号求和/平均、均值跳过 capacity=0 行、utilization 排序用近 N 天均值、top/page/page_size 默认与上限收敛)。租户校验平移 CDN tenantAccountIDs 模式,越权 account_id 返回 ErrNASAccountNotInTenant 映射 404 不泄露账号存在性;qc_status 原样透出并映射 data_status=zero_exception。DAO 新增 ListByFs/ListByAccounts 读方法(days 缺省 30 上限 90);路由注册于 /nas/metrics、/nas/top(与 /nas/:asset_id 共存);wire 装配 NASQueryService。

## Changes

### Files Created
- internal/cam/service/asset_nas_query.go
- internal/cam/service/nas_metric_query_test.go
- internal/cam/repository/dao/nas_metric_query.go
- internal/cam/web/asset_handler_nas_metrics.go
- internal/cam/web/asset_handler_nas_metrics_test.go

### Files Modified
- internal/cam/repository/dao/nas_metric.go
- internal/cam/web/asset_handler.go
- internal/cam/wire.go
- internal/cam/web/asset_handler_cdn_metrics_test.go

### Key Decisions
- Top 聚合在 service 层内存完成(行量级 ≤实例数×90 天):每日先取同日容量最大代表行再取最新日期行,均值亦按每日代表行计算,避免跨账号双计;items[].account_id 为去重升序账号列表(与 T11 前端对齐口径留待确认)
- 趋势端点响应在 proposal {fs_id, days[]} 基础上按 AC 增补 latest/average 两类值;utilization 为 0-1 fraction,capacity=0/缺失日为 null
- used>capacity 时 utilization 按 min(used,capacity) 收敛并打 warn 日志,原始 used 原样返回
- 越权账号在 service 层返回 ErrNASAccountNotInTenant,handler 统一映射 404(errs.AccountNotFound)而非 403
- top 端点 account_id 可缺省(=全部租户账号);metrics 端点 fs_id/account_id 必填,越界参数 400

## Test Results
- **Tests Executed**: Yes
- **Passed**: 47
- **Failed**: 0
- **Coverage**: 92.0%

## Acceptance Criteria
- [x] GET /assets/nas/metrics 返回 {fs_id, days[] 升序, date/capacity/used/utilization/data_status/qc_status}, 缺失日 data_status 标注不填假值, days 限 1~90, 返回最新一天+近 N 天均值, utilization 读取派生
- [x] GET /assets/nas/top sort∈capacity|utilization(utilization 用近 N 天均值), top 默认 10 最大 50, page/page_size 默认 1/10 最大 50, 响应 {total,page,page_size,items[]}
- [x] Top 按 fs_id 去重(日期 desc 再容量 desc 取第一行), 均值对无数据/capacity=0 跳过不记 0
- [x] 租户校验:account_id ∉ 租户账号集合返回 404 不泄露存在性
- [x] qc_status 闭环:zero_exception 原样暴露并映射 data_status, capacity=0 → utilization null
- [x] 单测覆盖租户隔离/fs_id 去重/分页/qc_status/utilization 边界, go build ./... 通过

## Notes
新增代码覆盖率:service GetFsMetrics 88.5%/GetTop 92.6%/聚合与派生辅助 100%,handler 93~100%(go tool cover 实测)。静态检查:go build ./... 通过、gofmt 无 diff、go vet ./... 干净。目标测试:service/web/dao/task/scheduler 全绿(47 PASS/0 FAIL 为 service+web 两包)。T11 前端对齐项:items[].account_id 跨账号并存时的呈现口径已在 keyDecisions 登记。

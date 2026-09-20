---
status: "completed"
started: "2026-09-20 13:06"
completed: "2026-09-20 13:17"
time_spent: "~11m"
---

# Task Record: 8 OSS 指标读取接口:单 bucket 趋势 + Top(租户校验 + 分页 + qc_status 闭环)

## Summary
实现 OSS 指标读取接口:GET /assets/oss/metrics(单 bucket 趋势)与 GET /assets/oss/top(账号视角 Top)。平移 NAS 读取三层先例:dao 新增 ListByBucket/ListByAccounts(OSSMetricDAO 接口扩展);service 新增 OSSQueryService(租户校验 account_id ∈ 租户账号集合越权 404、bucket_name 去重按「日期 desc 再容量 desc」取代表行不跨账号求和、days 1~90、top/page_size 默认 10 最大 50、sort ∈ storage_size|object_count 用近 N 天均值、latest+average 双口径、缺失日 data_status=missing 不填假值、qc_status=zero_exception 原样暴露并映射 data_status);web 新增 handler 并注册到既有 /oss 组(与 /oss/:asset_id 共存,冒烟测试防 panic)。wire.go 接线,既有 NewAssetHandler 调用点(wire/cdn测试/nas测试/nastest harness)补参。

## Changes

### Files Created
- internal/cam/service/asset_oss_query.go
- internal/cam/service/oss_metric_query_test.go
- internal/cam/repository/dao/oss_metric_query.go
- internal/cam/web/asset_handler_oss_metrics.go
- internal/cam/web/asset_handler_oss_metrics_test.go

### Files Modified
- internal/cam/repository/dao/oss_metric.go
- internal/cam/web/asset_handler.go
- internal/cam/web/asset_handler_nas_metrics.go
- internal/cam/wire.go
- internal/cam/web/asset_handler_nas_metrics_test.go
- internal/cam/web/asset_handler_cdn_metrics_test.go
- tests/nastest/harness.go

### Key Decisions
- sort 排序值用近 N 天均值口径(AC 明确「用近 N 天均值」),与 NAS 的 capacity 按 Latest 排序不同——遵循 OSS proposal 契约
- respondNASQueryError 重命名为 respondQueryError 并同时映射 NAS/OSS 越权错误为 404(DRY,NAS handler 行为不变)
- Top items[].account_id 暴露去重升序账号列表(与 NAS T11 前端对齐口径一致,T9 前端按此消费)
- web 层复用 parseNASBound/parseNASAccountID 解析器(参数边界与 NAS 完全同口径,避免复制)

## Test Results
- **Tests Executed**: Yes
- **Passed**: 24
- **Failed**: 0
- **Coverage**: 93.0%

## Acceptance Criteria
- [x] GET /assets/oss/metrics 返回 {bucket_name, days[]},升序+缺失日标注+days 1~90+latest/average 双口径
- [x] GET /assets/oss/top 参数校验(sort 枚举/top≤50/page/page_size≤50)与 {total,page,page_size,items[]} 响应
- [x] Top 按 bucket_name 去重(日期 desc 再容量 desc 取代表行),无数据实例跳过
- [x] 租户校验:account_id ∈ 租户账号集合,越权 404 不泄露存在性
- [x] qc_status 闭环:zero_exception 原样暴露并映射 data_status
- [x] 单测覆盖租户隔离/去重/分页/qc_status,go build ./... 通过

## Notes
静态检查:go build ./... 与 go vet 通过;make lint 因 Makefile 引号在 Git Bash 下解析失败(golangci-lint 未装时 Makefile 本就跳过 lint),以 go vet 兜底通过;make fmt 触碰大量既有文件但 git diff 零内容变更(纯 CRLF/LF 归一化),非本次改动不纳入提交。coverage 93% 为新 service 文件 go tool cover -func 加权值。

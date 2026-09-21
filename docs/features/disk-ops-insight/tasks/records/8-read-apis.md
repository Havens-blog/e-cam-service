---
status: "completed"
started: "2026-09-21 12:41"
completed: "2026-09-21 13:00"
time_spent: "~19m"
---

# Task Record: 8 Disk 指标读取接口:单盘趋势 + Top(租户校验 + 分页 + qc_status 闭环)

## Summary
实现 Disk 指标读取接口:GET /assets/disk/metrics(单盘趋势)与 GET /assets/disk/top(账号视角 Top)。DAO 新增 ListByDisk/ListByAccounts 读取方法;service 层 DiskQueryService 租户校验(resolveAccountScope 越权 ErrDiskAccountNotInTenant→handler 404)、disk_id 去重(日期 desc 再使用率 desc 取每日代表行,不跨账号求和/平均)、days 1~90/top≤50/page_size≤50 边界、latest+average 双口径、qc_status 原样暴露+data_status 映射并结合 usage_scope 甄别(busy_share 合法闲盘 0→ok 且参与均值;口径缺失 0→zero_exception 且均值跳过);handler 注册 /disk/metrics、/disk/top 与既有 /disk、/disk/:asset_id 共存(冒烟测试防 gin 路由 panic);wire 注入 DiskQueryService。

## Changes

### Files Created
- internal/cam/repository/dao/disk_metric_query.go
- internal/cam/service/asset_disk_query.go
- internal/cam/web/asset_handler_disk_metrics.go
- internal/cam/service/disk_metric_query_test.go
- internal/cam/web/asset_handler_disk_metrics_test.go

### Files Modified
- internal/cam/repository/dao/disk_metric.go
- internal/cam/web/asset_handler.go
- internal/cam/web/asset_handler_nas_metrics.go
- internal/cam/wire.go
- internal/cam/web/asset_handler_nas_metrics_test.go
- internal/cam/web/asset_handler_oss_metrics_test.go
- internal/cam/web/asset_handler_cdn_metrics_test.go
- tests/nastest/harness.go
- tests/osstest/harness.go

### Key Decisions
- qc_status 读取侧甄别按 T2 记录实现:diskDataStatus/diskIsMissingScopeZero 以 usage_scope 区分「busy_share 合法闲盘 0(→ok,参与均值)」与「口径缺失 0(→zero_exception,均值跳过)」,qc_status 原样透出
- Top 三排序键(usage_percent|iops|throughput)统一用近 N 天均值口径(规格明示),区别于 NAS 容量用最新天;每日代表行=同日 usage_percent 最大,均值按代表行计算避免共享盘双计
- 响应在 AC 字段基础上增列 usage_scope(趋势点与 Top item),前端据此区分口径呈现(proposal AC-5 定案)
- DAO 读取方法并入 DiskMetricDAO 接口(与 NAS/OSS 同型);读取侧复用 metricDateFloor 窗口口径

## Test Results
- **Tests Executed**: Yes
- **Passed**: 95
- **Failed**: 0
- **Coverage**: 92.8%

## Acceptance Criteria
- [x] GET /assets/disk/metrics 返回 {disk_id, days[]}:升序、date/usage_percent/iops/throughput/data_status/qc_status、缺失日标注不填假值、days 1~90、latest+average
- [x] GET /assets/disk/top:sort ∈ usage_percent|iops|throughput(均值口径)、top 默认 10 最大 50、分页默认 1/10 最大 50、响应 {total,page,page_size,items[]}
- [x] Top 按 disk_id 去重(日期 desc 再使用率 desc 取第一行),无数据实例跳过不记 0
- [x] 租户校验:tenantID 来自鉴权上下文,account_id ∈ 租户账号集合,越权 404
- [x] qc_status 闭环:zero_exception 原样暴露映射 data_status,结合 usage_scope 甄别 busy_share 合法闲盘 0
- [x] 单测:租户隔离/disk_id 去重/分页 limit/qc_status 传递;go build ./... 通过

## Notes
coverage 92.8 为新增代码覆盖率(service 新文件 15 函数均值 92.8%,handler 新文件 96.5%,均超 80% 门槛;整包值为 service 55.7%/web 13.3% 因含大量存量未覆盖 handler)。golangci-lint 未安装按 Makefile 回退跳过;go vet 通过;gofmt -s 对全部改动文件 clean。respondQueryError 扩展支持 ErrDiskAccountNotInTenant;NewAssetHandler 增加 diskQuery 参数并同步更新 7 处既有调用点(wire+nastest/osstest harness+5 测试文件)。

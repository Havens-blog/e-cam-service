---
status: "completed"
started: "2026-09-19 16:43"
completed: "2026-09-19 17:03"
time_spent: "~20m"
---

# Task Record: 5 nas:collect_metrics 采集执行器(账号遍历 + 首写生效 + 注册)

## Summary
实现 nas:collect_metrics 采集执行器(账号遍历 + 首写生效 + 注册):新增 SyncNASMetricsExecutor——按活跃云账号(不依赖 EnableAutoSync)从 ecam_instance 枚举 NAS 实例(以本地资产枚举为准,region 取 attributes["region"]),调 NASMetricQuerier 采集 [昨日,今日] 日指标;今日行走 DAO 新增 BulkInsertIfAbsent($setOnInsert 首写生效,仅保护今日行),昨日及更早行走 BulkUpsertMetrics 覆盖更新(次日补采显式覆盖);不继承 CDN 全零跳过过滤(capacity=0 照常入批,由 DAO 数量级自检打 zero_exception 落库可见);账号级互斥 tryAcquireAccount/releaseAccount 与 CDN 执行器同模式;已注册到 internal/cam/task/module.go(nas:collect_metrics)。

## Changes

### Files Created
- internal/cam/task/executor/sync_nas_metrics.go
- internal/cam/task/executor/sync_nas_metrics_test.go

### Files Modified
- internal/cam/repository/dao/nas_metric.go
- internal/cam/repository/dao/nas_metric_test.go
- internal/cam/repository/dao/nas_metric_live_test.go
- internal/cam/task/module.go

### Key Decisions
- 首写生效在 DAO 层落实:新增 BulkInsertIfAbsent(更新文档仅含 $setOnInsert,唯一键命中已存在行不修改),执行器按 m.Date==今日 分流两批写入,符合任务 Implementation Notes「DAO 层 $setOnInsert 保护今日行」
- 实例枚举以 ecam_instance 为准(instanceRepo.Search AssetTypes=[nas],经 ModelUIDPatternFor 命中 cloud_nas/*_nas),不调云端 ListInstances;活跃账号=枚举结果 ≥1 实例,无实例账号计入 accounts_without_nas 不采集
- 采集区间默认 days=2([昨日,今日],Asia/Shanghai 运营时区),endDate 即「今日」作为首写/覆盖分流边界;上限 31 天
- 单实例采集有界并发 5(semaphore,沿 CDN 执行器先例);Result 携带 metrics_total/accounts_without_nas/no_metric_support/skipped_accounts/failed_instances(失败计数+末次错误明细与连续零成功告警按任务说明留待 T6)

## Test Results
- **Tests Executed**: Yes
- **Passed**: 11
- **Failed**: 0
- **Coverage**: 82.0%

## Acceptance Criteria
- [x] 执行器按活跃账号(ecam_instance 枚举 ≥1 个 NAS 实例)遍历,不依赖 EnableAutoSync
- [x] 每日采集区间 [昨日,今日]:补昨日覆盖更新 + 今日首写生效;同日重复采集不产生脏行
- [x] 不继承 CDN 全零跳过过滤:capacity=0 异常行落库可见(qc_status=zero_exception)
- [x] 注册到 internal/cam/task/module.go(nas:collect_metrics),账号级互斥避免同账号并发踩踏
- [x] 单测:活跃账号筛选、首写生效 vs 昨日覆盖、零容量行落库可见、账号互斥
- [x] go build ./... 通过

## Notes
coverage 82.0 为新增 sync_nas_metrics.go 函数级覆盖率(go test -coverprofile + go tool cover -func 加权均值;GetType 0% 为一行转发)。目标测试:go test ./internal/cam/task/executor/ ./internal/cam/repository/dao/ 全绿(含既有用例无回归);DAO live 测试(TestNASMetricInsertIfAbsentLive)已就位,本机未设 MONGO_DSN 按 T2 既有约定 skip。go build ./... 唯一失败源为并行会话未跟踪 WIP 包 internal/logquery/service(diagnose.go undefined symbols,非本任务产物,依 proposal 约束不动并行 WIP);排除该包全仓构建通过。make lint 目标在本机 shell 存在既有引号 bug(unexpected EOF)且 golangci-lint 未安装,以 go vet(全绿)替代。make fmt 触发的全仓 CRLF 噪音与既有格式漂移不纳入本任务提交范围。

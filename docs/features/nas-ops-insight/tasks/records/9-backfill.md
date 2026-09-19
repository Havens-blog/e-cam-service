---
status: "completed"
started: "2026-09-19 17:51"
completed: "2026-09-19 18:21"
time_spent: "~30m"
---

# Task Record: 9 一次性历史回填任务(配额节流 + 错峰 + 幂等去重)

## Summary
实现一次性 NAS 历史指标回填任务 nas:backfill_metrics:新增 SyncNASBackfillExecutor(区间 [昨日-(N-1), 昨日],N 钳位 14~90 默认 30,今日行归每日采集不触碰);按「厂商×账号」分片、批 ≤5 实例 × ≤10 天,批间退避 5s 起、限流指数退避封顶 5min、重试 3 次耗尽挂起该厂商其余厂商续跑;CloudWatch GetMetricData 50000 点/60s 按 30% 余量换算 35000 预算并执行时防御校验;错峰窗口 [01:30, 06:00) Asia/Shanghai(窗口外触发跳过、中途到期挂起);幂等去重:每批 ListExistingMetricDates 预检,已成功批次零调用零写入、只对缺失连续段回源补采,重跑不产生脏行。调度侧新增 auto_sync_nas_backfill.go:窗口内经持久化日闸(nas_backfill)每日提交一次,实现限流/窗口挂起后次日窗口自动续跑。执行器注册进 task/module.go;DAO 新增 ListExistingMetricDates(唯一索引前缀投影查询);实例枚举抽为 listAccountNASInstancesFromRepo 供日采/回填共用。所有节流/退避/窗口/配额参数落地为 const 配置块(Hard Rule)。

## Changes

### Files Created
- internal/cam/task/executor/sync_nas_metrics_backfill.go
- internal/cam/task/executor/sync_nas_metrics_backfill_test.go
- internal/cam/scheduler/auto_sync_nas_backfill.go
- internal/cam/scheduler/auto_sync_nas_backfill_test.go

### Files Modified
- internal/cam/repository/dao/nas_metric.go
- internal/cam/repository/dao/nas_metric_live_test.go
- internal/cam/task/executor/sync_nas_metrics.go
- internal/cam/task/module.go
- internal/cam/scheduler/auto_sync.go

### Key Decisions
- 回填区间止于昨日:今日行归每日采集首写生效语义,回填全走 BulkUpsertMetrics 覆盖更新路径,与日采错峰不碰撞
- 幂等续跑以 DAO 预检实现:已成功批次(区间全落库)不重试厂商 API,缺失日期切连续段回源,凭 (account_id, fs_id, date) 唯一键重跑不产生脏行
- 限流挂起策略:单缺失段指数退避 5s→10s→20s 重试 3 次,耗尽挂起整个厂商(其余厂商续跑),次日窗口由调度日闸自动续跑
- 调度器 AutoSyncScheduler 增加 nowFn 注入点,错峰窗口判定可测(无真实时钟依赖)
- provider/account_id 限定在执行器侧再收口一次,不依赖仓储实现差异

## Test Results
- **Tests Executed**: Yes
- **Passed**: 23
- **Failed**: 0
- **Coverage**: 93.5%

## Acceptance Criteria
- [x] 回填任务 nas:backfill_metrics 从厂商 API 拉取启用日前 N 天(14~90,默认 30)历史写入 ecam_nas_metric
- [x] 配额节流:厂商×账号分片;批 ≤5 实例 × ≤10 天;批间退避 5s 起遇限流指数退避至上限;CloudWatch 配额换算留 30% 余量
- [x] 错峰:回填在 01:30~06:00 窗口执行与每日采集不碰撞;命中限流的厂商挂起、次日窗口续跑
- [x] 幂等去重:已成功批次不重试((account_id, fs_id, date) 唯一键幂等),重跑不产生脏行
- [x] 单测:分片参数、退避逻辑、幂等重跑、错峰窗口判定
- [x] go build ./... 通过

## Notes
Hard Rules 双条满足:批大小/退避/窗口/配额参数全部为 const 配置块;已成功批次凭 ListExistingMetricDates 预检不重试。验证:go build ./... 通过;go vet ./... 干净;gofmt 无 diff;targeted 测试 executor/scheduler/dao 三包全绿(23 新测试含 3 调度测试 + 1 MONGO_DSN 门控 DAO 活体测试);新回填文件 18 函数平均覆盖 93.5%。make lint 因 golangci-lint 未安装按 Makefile 设计跳过(既有环境状态,非本次引入)。回填完成后后续窗口提交为去重空跑(仅本地枚举+预检查询,不调厂商 API),兼作新纳管账号补历史。

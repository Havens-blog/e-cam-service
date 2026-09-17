---
status: "completed"
started: "2026-09-17 11:28"
completed: "2026-09-17 11:53"
time_spent: "~25m"
---

# Task Record: 4 cert:cert-import 调度点接线

## Summary
cert:cert-import 调度点接线：jobs.go 新增第 9 个调度点（JobCertImport + SpecCertImportDaily "0 1 * * *"，01:00 与 scan 02:00 错峰）、窄端口 CertificateSyncer（SyncCertificates 形态）+ 编译期断言接线任务 3 service.CertSyncService、CertJobs 新增 Sync 字段；包注释映射表更新为 10 类任务 → 9 个调度点；ioc/cert.go initCertJobs 挂载 Sync: certModule.CertSyncSvc（cronjob.enabled 门控复用既有 InitJobs 链，未改动）。调度层零业务逻辑内联（Hard Rule）。

## Changes

### Files Created
无

### Files Modified
- internal/cert/scheduler/jobs.go
- internal/cert/scheduler/jobs_test.go
- ioc/cert.go

### Key Decisions
- SPEC CONTRADICTION resolution: module_test.go（Hard Rule 文件清单外）构造 scheduler.CertJobs 不带 Sync 字段且逐 spec Run——cert-import 入口做 nil 容忍（未装配降级跳过 + slog.Warn，对齐 cert 域可选端口降级口径如 ioc dnsSource=nil probe 回退），module_test.go 零改动保持 Hard Rule 文件清单约束；经 TestInitCertModule_BootSmoke（CERT_TEST_MONGODB_DSN 实例）运行期实证
- 调度侧 ErrSyncRunning（手工轮 running 中）按预期让位跳过不上抛——对齐 scan ErrScanInProgress 口径（proposal: 与 probe/scan 防重模式一致）；CAS 防重本体语义由任务 3 服务层测试承载（TestCertSync_CASGuard），调度层不重复断言（衔接信息要求）
- ioc/cert.go import 顺序 gofmt 修正（certservice/scheduler 排序，属既有漂移但文件在本任务清单内）

## Test Results
- **Tests Executed**: Yes
- **Passed**: 37
- **Failed**: 0
- **Coverage**: 92.6%

## Acceptance Criteria
- [x] jobs.go 新增 JobCertImport 调度点 + SpecCertImportDaily = "0 1 * * *"（01:00，与 scan 02:00 错峰）
- [x] 窄端口 CertificateSyncer（SyncCertificates(ctx) (SyncRun, error) 形态）编译期接线任务 3 同步服务
- [x] CAS 防重：running 中再次触发不启动第二轮（调度侧让位语义单测 + 服务层 CAS 测试已绿）
- [x] ioc/cert.go InitCertJobs 按 8→9 点清单挂载 cert:cert-import（装配后启动不报错；cronjob.enabled 同门控）

## Notes
测试计数口径：scheduler 包 24（含新增 TestCertJobs_CertImportYieldsToRunning / TestCertJobs_CertImportSkipsUnwiredSync，全绿）+ service 包 TestCertSync* 回归 11 + cert 包模块级冒烟 2（TestInitCertModule_BootSmoke / TestUnavailableDispatcher，CERT_TEST_MONGODB_DSN 指向可达实例）；ioc 包 gin_test ok（未计数，不进总数）。coverage 92.6% = scheduler 包单包 -cover（覆盖目标 80% 已达，停止加测）。AC-4 '装配后启动不报错' 以编译期验证（go build ./... + wire 图）+ 模块级冒烟近似——全进程 InitApp boot 本机不可行（无本地 etcd/redis/ldap，7.1 先例）。go build -p 1 ./... 门禁通过（期间观察到并行 LQO 会话在 internal/shared/cloudx/logquery/tencent 的在途半成品造成瞬时编译破，待其自愈后复跑全绿，与本任务改动无关）。未用 -race（宿主无 cgo/gcc，仓约束）。Hard Rule 文件清单外的既有注释漂移未动（留待后续）：ioc/jobs.go:14 与 internal/cert/module.go:42 仍写'9 类任务'（现为 10 类任务/9 调度点）。fmt 门禁经 CR 剥离临时文件法核对三文件 gofmt-delta=0；三文件 U+FFFD 扫描 clean。

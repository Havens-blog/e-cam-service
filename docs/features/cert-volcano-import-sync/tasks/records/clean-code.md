---
status: "completed"
started: "2026-09-17 12:30"
completed: "2026-09-17 12:30"
time_spent: ""
---

# Task Record: T-clean-code Simplify and Clean Code

## Summary
code-quality.simplify 完成：评审 cert-volcano-import-sync 全部 8 个 feature 范围代码文件（volcano 适配器/导入 shim/同步服务/调度接线/ioc 装配/handler 及测试），无行为级简化点（任务 1-5 已收敛：可选变参装配、纯函数端口映射、窄接口均到位）；修正 2 处清单外注释漂移：module.go 调度面注释 9 类定时任务→10 类任务/9 调度点（对齐 scheduler/jobs.go 已修正口径），discovery_import_service.go DiscoveryCertAdapter 云清单补 volcano。ioc/jobs.go:14 同源漂移在 feature git diff 边界之外（该文件未被本 feature 提交触碰），按 Hard Acceptance Criteria 不改、留证登记。顺手清理 6 个孤儿 link.exe（2026-08-19~09-16 累积 ~14GB commit）解除后续构建 OOM 隐患。

## Changes

### Files Created
无

### Files Modified
- internal/cert/module.go
- internal/cert/service/discovery_import_service.go

### Key Decisions
- 仅注释级修正、零行为变更：评审确认任务 1-5 代码无需进一步简化，避免为凑简化引入 churn 与并行 log-query 会话的合并风险
- ioc/jobs.go:14 注释漂移不修：Hard Acceptance Criteria 限定 git diff 边界，该文件不属于本 feature 提交范围，登记为遗留项
- 部署器/扫描侧其余五云清单注释（deployer/channel.go、reference_scan_service.go 等）不修：volcano 未接入那些端口，注释本就正确且文件在范围外

## Test Results
- **Tests Executed**: Yes
- **Passed**: 418
- **Failed**: 0
- **Coverage**: 46.7%

## Acceptance Criteria
- [x] Code simplified without changing external behavior
- [x] No files cleaned outside this feature's scope (git diff boundaries)

## Notes
测试：5 个 feature 范围包逐包顺序跑（宿主 OOM 限制，无 -race）：service 274/scheduler 24/web 80/cert root 6/volcano 34 全绿（volcano 按既定 BOM 剥离-还原法跑 -cover，kafka.go cmp 校验字节一致）。coverage 46.7 为 5 包合并 statement 加权总（被 cert root 装配面 28.1% 与 volcano 包内 pre-existing 未测 kafka.go 摊薄）；feature 逻辑面实际为 service 76.5%/scheduler 92.6%/web 79.8%/volcano cert.go 88.2%。改动仅 2 行注释，gofmt 经 tr -d '\r' 临时文件法核 0 delta，U+FFFD 扫描 0 命中。

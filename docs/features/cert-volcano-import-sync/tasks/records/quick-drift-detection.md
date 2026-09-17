---
status: "completed"
started: "2026-09-17 14:39"
completed: "2026-09-17 14:50"
time_spent: "~11m"
---

# Task Record: T-quick-doc-drift Detect Spec Drift

## Summary
Drift-only 模式核对 cert-volcano-import-sync：项目级 spec 目录（docs/business-rules/、docs/conventions/）不存在，无漂移对象；对 proposal.md 全部声明逐项对照实现（commits 3c65d91→5a217c3）验证 9/9 全项一致、无夸大（调度 spec cert:cert-import 0 1 * * *、端点 POST /api/v1/certs/discovery/sync、409 CERT_SYNC_IN_PROGRESS、uk_fp_cloud_account 唯一键、uploadedAt 降序反查、ErrDuplicateFingerprint 幂等 success、增量判定层跳过、只读纪律构造性保证、依赖声明与分页对齐）。唯一漂移在 feature 文件边界外的代码注释：ioc/jobs.go:14 与 :111 两处「cert 域 9 类任务」应为 10 类定时任务（9 个调度点，权威计数 module.go:42/scheduler/jobs.go/ioc/cert.go:60）——按边界纪律如实登记、未越界修复（:111 为本次新增发现）。核验表以 Drift Verification 段落落 proposal 末尾，[auto-specs] 提交 8c97409。

## Changes

### Files Created
无

### Files Modified
- docs/proposals/cert-volcano-import-sync/proposal.md
- docs/.vocabulary.md

### Key Decisions
无

## Document Metrics
claims verified: 9/9 current, 0 proposal drift; drift registered: 2 (out-of-scope code comments ioc/jobs.go:14,:111); project-level spec files: 0 (dirs absent); proposal additions: +29 lines (Drift Verification section); vocabulary: regenerated empty-state (4 knowledge dirs absent)

## Referenced Documents
- docs/features/cert-volcano-import-sync/tasks/quick-drift-detection.md
- docs/proposals/cert-volcano-import-sync/proposal.md
- internal/cert/scheduler/jobs.go
- internal/cert/service/cert_sync_service.go
- internal/cert/web/discovery_handler.go
- internal/shared/cloudx/volcano/cert.go
- internal/cert/domain/cloud_cert_mapping.go
- internal/cert/repository/cloud_cert_mapping.go
- ioc/jobs.go
- docs/features/cert-volcano-import-sync/testing/latest.md

## Review Status
final

## Acceptance Criteria
- [x] git diff 圈定 feature 文件范围（main...HEAD 为空→改用 feature 提交集 3c65d91…c732d1e）
- [x] docs/business-rules/ 与 docs/conventions/ 存在性检查（均不存在→无漂移对象，drift-only 模式）
- [x] proposal 声明逐项对照代码验证（调度 spec/错误码/端点路径/幂等语义/依赖，9/9 一致）
- [x] 越界漂移如实登记不修复（ioc/jobs.go:14 已知遗留 + :111 新增发现）
- [x] spec 变更以 [auto-specs] 标签提交（8c97409，仅 2 个目标文件，git show --name-only 核对无跨 feature 卷入）

## Notes
1) ioc/jobs.go 漂移两处（:14、:111）均为代码注释而非 spec 文件，超出本任务 auto-fix 权限面（任务文件授权修的是 spec 文件），且衔接信息明确不要求越界修复；已登记于 proposal Drift Verification 段供后续同域任务顺手修。2) proposal 既有 statements（如 Evidence 里「已注册 8 个调度点」）为时间锚定的前 feature 事实陈述，非漂移。3) docs/.vocabulary.md 按幂等规则整文件再生成（四个知识目录均不存在，仅 generated 日期刷新）。4) SC 复选框保持未勾选态：本任务职责是漂移核对而非 SC 验收勾选，逐项判定以核验表承载。

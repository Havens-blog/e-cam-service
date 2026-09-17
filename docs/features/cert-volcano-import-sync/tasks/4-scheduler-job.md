---
id: "4"
title: "cert:cert-import 调度点接线"
priority: "P1"
estimated_time: "1h"
complexity: "medium"
dependencies: ["3"]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 4: cert:cert-import 调度点接线

## Description

把任务 3 的同步服务以天级节奏接入现有调度面：`internal/cert/scheduler/jobs.go` 新增第 9 个调度点 `cert:cert-import`（01:00，与 scan 02:00 错峰），经窄端口对接，`ioc/cert.go InitCertJobs` 挂载。只做接线，不内联业务逻辑（与既有 8 点一致）。

## Reference Files

- `docs/proposals/cert-volcano-import-sync/proposal.md` — 调度接线（Proposed Solution / In Scope / Success Criteria）
- `internal/cert/scheduler/jobs.go` — 调度点注册模式（JobScan 等 8 点：名称常量 + spec + 窄端口 + 编译期断言）
- `internal/cert/scheduler/inspection_job.go` — 任务函数实现/窄端口实现示例
- `ioc/cert.go` — InitCertJobs 挂载模式（cronjob.enabled 同门控）

## Acceptance Criteria

- [ ] `jobs.go` 新增 `JobCertImport` 调度点 + `SpecCertImportDaily = "0 1 * * *"`（01:00，与 scan 02:00 错峰）
- [ ] 窄端口 `CertificateSyncer`（`SyncCertificates(ctx) (SyncRun, error)` 形态）编译期接线任务 3 同步服务
- [ ] CAS 防重：running 中再次触发不启动第二轮（单测断言第二次返回 `ErrSyncRunning` 或等义语义）
- [ ] `ioc/cert.go InitCertJobs` 按 8→9 点清单挂载 `cert:cert-import`（装配后启动不报错；cronjob.enabled 同门控）

## Hard Rules

- 仅修改以下文件：`internal/cert/scheduler/jobs.go`、`internal/cert/scheduler/jobs_test.go`、`ioc/cert.go`。
- 调度点不内联业务逻辑——只窄端口转交任务 3 服务（对齐包注释 Hard Rule）。

## Implementation Notes

### 包注释对齐

更新 `scheduler/jobs.go` 包注释「9 类任务 → 8 个调度点」的映射表为 10 类任务、9 个调度点（新增 cert:cert-import 行），保持文档与代码一致。

### Key Risks 缓解

- 调度与手动触发同刻：防重守卫（CAS）保证不并发跑两轮；手动导入（DiscoveryHandler.Import）与同步轮竞态由幂等消化（任务 3 测试覆盖）。

### Test Impact

- `jobs_test.go` 新增调度点注册与防重断言；现有 8 点测试全绿。风险 low。
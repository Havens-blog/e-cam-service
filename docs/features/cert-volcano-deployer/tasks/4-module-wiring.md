---
id: "4"
title: "module.go 第 6 云装配 + 变更清单回归"
priority: "P1"
estimated_time: "1h"
complexity: "medium"
dependencies: ["2", "3"]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 4: module.go 第 6 云装配 + 变更清单回归

## Description

把火山部署器与扫描适配器接入模块：`module.go` `RegisterDeployer`（volcano×4 产品）+ 扫描适配器列表加入火山（第 6 云）。随后回归验证：变更清单生成对火山引用不再 skipped（`AutoChangeable=true`）——这是「闭环打通」的装配面收口。

## Reference Files

- `docs/proposals/cert-volcano-deployer/proposal.md` — In Scope / Success Criteria
- `internal/cert/module.go` — RegisterDeployer（5 云 136-165 行）与扫描适配器装配（193-197 行）
- `internal/cert/service/changelist_generator_test.go` — 变更清单生成回归测试（五云引用可执行断言先例）
- `docs/features/cert-multicloud-deployers/tasks/records/` — 三云清单回归先例（TestGenerateChangeList_ThreeCloudReferencesExecutable）

## Acceptance Criteria

- [ ] `RegisterDeployer` 注册 volcano×4 产品（CDN/WAF/ALB/NLB），与 5 云并列；CertAdapter 共享装配（对齐 5 云 shared 模式）
- [ ] 扫描适配器列表加入火山项（`NewVolcanoScanAdapter`）
- [ ] 清单生成回归：火山引用 `AutoChangeable=true`（不再 `ERR_DISCOVERY_ONLY`/skipped），新增测试用例断言
- [ ] 既有五云部署/清单/验证测试全绿（第 6 云不破坏现有行为）

## Hard Rules

- 仅修改以下文件：`internal/cert/module.go`、`internal/cert/module_test.go`、变更清单生成测试文件（新增用例）。
- 装配不内联业务逻辑：仅注册/接线。

## Implementation Notes

### 装配对齐

- 参照 cert-multicloud-deployers 的 module.go 装配（5 个 RegisterDeployer + shared CertAdapter）：火山 4 产品注册后模块清单为 6 云。
- `discoveryOnlyClouds` 判定：火山注册部署器后不再触发该分区（对齐三云落地先例）。

### File Scope

修改：`internal/cert/module.go`、`internal/cert/service/changelist_generator_test.go`（或等价回归测试文件）+ 需要时的 module_test.go。

### Test Impact

- 新增清单生成用例（火山引用 AutoChangeable）；既有五云清单测试全绿。风险 low。
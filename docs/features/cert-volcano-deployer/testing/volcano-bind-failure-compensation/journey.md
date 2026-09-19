---
feature: "cert-volcano-deployer"
journey: "volcano-bind-failure-compensation"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-volcano-deployer/proposal.md
generated: "2026-09-19"
existing_tests:
  - tests/volcano-cert-replacement/volcano_cert_replacement_test.go::TestVolcanoCertReplacement_BindFailureCompensation
---

# Journey: volcano-bind-failure-compensation

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

两段式第二段（BindResource）失败后的补偿闭环：item 标记失败 → `CleanupOrphan` 幂等清理第一段已上传的孤儿证书 → 映射 `active→orphan` 入清理队列（复用既有状态机）。火山与五云同一补偿语义，差异仅在清理落到火山 certificateservice `DeleteInstance`。补偿幂等是硬约束（双调用同结果）。(Source: proposal.md Key Scenario "失败模式" + Success Criteria 5；状态回写 + 云侧删除故定级 High)

## Setup

- 两段式第一段已成功：云证书已上传火山证书库、映射落 `active`
- 第二段绑定可注入失败（fake SDK 桩返回绑定错误）
- 清理队列与映射状态机可用

## Happy Path

### Step 1: 绑定失败落账

**User Action**: 触发批次执行，注入第二段绑定失败

**Expected Result**: item 状态失败，错误呈报为静态 reason（云侧错误细节仅日志不进响应），编排立即进入补偿分支

### Step 2: 补偿清理孤儿

**User Action**: 编排自动触发 `CleanupOrphan`

**Expected Result**: 第一段上传的孤儿证书经 `DeleteInstance` 删除，映射 `active→orphan` 入清理队列，不留悬空 active 引用

### Step 3: 幂等复验

**User Action**: 重复调用 `CleanupOrphan`（双调用）

**Expected Result**: 双调用同结果——第二次为幂等空成功（已删除即视为清理完成），无报错无状态翻转

## Edge Cases

### Step 1b: 失败项批次内隔离

**Precondition**: 同批含多个产品项，仅一项绑定失败

**User Action**: 执行批次

**Expected Result**: 失败项补偿独立完成，其余项不受牵连（批次内失败隔离，见 journey volcano-two-phase-replacement Step 4b）

### Step 2b: 清理 API 本身失败

**Precondition**: `DeleteInstance` 调用失败（云侧瞬时错误）

**User Action**: 触发补偿

**Expected Result**: 孤儿保留在清理队列（`orphan` 态不回退 `active`），后续轮次重试收敛；补偿失败不掩盖绑定失败原因

### Step 2c: 补偿与重试执行竞争

**Precondition**: item 失败补偿进行中，操作者并发触发重试执行

**User Action**: 并发操作

**Expected Result**: 既有状态机语义收敛（复用五云并发口径），映射不出现 active/orphan 双态并存

### Step 3b: 孤儿已被云侧人工删除

**Precondition**: 清理队列含孤儿条目，但云侧证书已被控制台手动删除

**User Action**: 触发清理

**Expected Result**: 幂等语义覆盖"不存在即成功"（DeleteInstance 语义幂等），队列收敛，不误报失败

## Journey Invariants

- 绑定失败必触发补偿，不留悬空 active 映射
- `CleanupOrphan` 幂等：双调用同结果
- 补偿失败不影响既有失败原因呈报（静态 reason，云侧细节仅日志）
- 孤儿清理走同一映射状态机（active→orphan→清理队列），火山不引入新状态

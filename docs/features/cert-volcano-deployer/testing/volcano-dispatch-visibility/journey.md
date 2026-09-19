---
feature: "cert-volcano-deployer"
journey: "volcano-dispatch-visibility"
risk_level: "Medium"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-volcano-deployer/proposal.md
generated: "2026-09-19"
existing_tests:
  - tests/volcano-cert-replacement/volcano_cert_replacement_test.go::TestVolcanoChannelDispatch_ProductVisibility
---

# Journey: volcano-dispatch-visibility

**Risk Level**: Medium

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

第 6 云装配后的可见性与分发面：火山引用经扫描适配器进入引用扫描（只读）→ 清单生成判定 `AutoChangeable=true`（回归：不再 `ERR_DISCOVERY_ONLY`/skipped）→ 执行时按云/产品通道分发命中火山部署器（四产品可见，不误路由）。扫描与清单判定无不可逆副作用（执行写操作归 journey volcano-two-phase-replacement），故定级 Medium。(Source: proposal.md In Scope "module.go 装配" + "变更清单回归" + Success Criteria 2/3/8)

## Setup

- `module.go` 装配完成：`RegisterDeployer` volcano×4 + 扫描适配器列表加入火山（第 6 云）
- 火山账号已登记、`accountScanSource.ActiveByCloud` 支持火山枚举
- 线上存在火山四产品引用旧证书的资源；同时存在既有五云引用（回归对照面）

## Happy Path

### Step 1: 火山引用进入扫描

**User Action**: 触发引用扫描，扫描适配器枚举火山四产品资源

**Expected Result**: 火山资源进入 `CertReference` 面（指纹解析对齐 3.5 口径：映射反查 → GetCert 要素 → 确定性占位指纹），扫描全程只读无云侧写操作

### Step 2: 清单生成判定可执行

**User Action**: 清单生成评估火山引用

**Expected Result**: 火山引用 `AutoChangeable=true`（cloud_api 通道），不再 `ERR_DISCOVERY_ONLY`/skipped；清单项 `Target.Cloud=volcano` 且产品归属正确

### Step 3: 执行分发命中火山部署器

**User Action**: 确认并执行含火山项的变更单

**Expected Result**: 火山目标经云/产品通道分发命中火山部署器（四产品逐项可见），不落入其他云部署器；执行进度按 item 如实可见

## Edge Cases

### Step 1b: 扫描期火山 API 失败隔离

**Precondition**: 火山扫描 API 调用失败

**User Action**: 触发引用扫描

**Expected Result**: 失败以静态 reason 隔离呈现（云级隔离，对齐五云失败隔离先例），其他云扫描结果不受影响

### Step 1c: 未知产品资源形态

**Precondition**: 扫描遇到四产品之外/形态未知的火山资源

**User Action**: 解析该资源指纹

**Expected Result**: 走确定性占位指纹（`certscan-unresolved:` 语义一致），不 panic 不误判指纹

### Step 2b: 五云回归不受第 6 云装配影响

**Precondition**: 既有五云引用与新装配共存

**User Action**: 清单生成并核对五云项

**Expected Result**: 五云项判定与装配前一致（`AutoChangeable`/通道/部署器路由均无漂移），第 6 云新增零破坏

### Step 3b: 非 volcano 目标不误路由

**Precondition**: 变更单混合火山与五云项

**User Action**: 执行变更单

**Expected Result**: 五云项命中各自既有部署器，火山项命中火山部署器，通道互不误路由

### Step 3c: 火山部署器未装配降级

**Precondition**: 火山 deployer 未注册（装配缺失/被摘除）

**User Action**: 清单生成评估火山引用

**Expected Result**: 回落既有 skipped/不可执行语义（不误报可执行），呈报原因可诊断，服务不 panic

## Journey Invariants

- 引用扫描只读；写操作仅存在于执行面
- 火山引用判定回归恒真：AutoChangeable=true，不再 ERR_DISCOVERY_ONLY/skipped
- 通道分发按云/产品精确路由，跨云不误路由
- 第 6 云装配对既有五云行为零破坏（回归恒绿）
- 云侧扫描失败静态 reason 隔离，不进响应细节

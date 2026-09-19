---
feature: "cert-volcano-deployer"
journey: "volcano-rollback-verify-window"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-volcano-deployer/proposal.md
generated: "2026-09-19"
existing_tests:
  - tests/volcano-cert-replacement/volcano_cert_replacement_test.go::TestVolcanoCertReplacement_RollbackRestoresOldCertPerProduct
notes: "新增可测面（功能级 tests/ 未单独覆盖）：GetCert 映射回退（WAF/ALB/NLB 无指纹通道时回滚目标有效性判定）。"
---

# Journey: volcano-rollback-verify-window

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

验证窗口异常 → 回滚恢复旧证书的闭环：`ProbeDomains` 拨测发现线上指纹不符 → 回滚发起 → `GetCert` 三判定校验旧云证书 ID 有效（回滚目标有效性判定）→ `BindResource` 恢复旧 ID（四产品逐产品覆盖）。WAF/ALB/NLB 无独立指纹通道，`GetCert` 走映射回退解析旧 ID——这是火山新增可测面。回滚语义云无关，与五云同一状态机。(Source: proposal.md Key Scenario "回滚" + Success Criteria 7 + Constraints（GetCert 依赖 cert-volcano-import-sync 证书库适配器）；状态回写 + 资源绑定回退故定级 High)

## Setup

- 变更单处于验证窗口（某批次绑定已执行，非终态）
- 旧证书云 ID 在映射中可查（替换前映射）
- 火山证书库可用（fake SDK 桩承载 GetCert 查询与绑定回退）

## Happy Path

### Step 1: 验证窗口拨测发现异常

**User Action**: 验证窗口内对目标域名执行 `ProbeDomains` TLS 拨测

**Expected Result**: 拨测线上指纹 ≠ 新证书指纹（仍为旧指纹或不符合预期），变更单停留验证窗口，回滚入口可用

### Step 2: 回滚发起与 GetCert 目标校验

**User Action**: 运维发起回滚，编排经 `GetCert` 校验旧云证书 ID 有效（三判定：在库、可用、与台账指纹一致）

**Expected Result**: 旧 ID 判定有效；WAF/ALB/NLB 无指纹通道场景经映射回退解析旧 ID 与要素，判定不因通道缺失而失败

### Step 3: 逐产品恢复旧绑定

**User Action**: 编排对四产品分别执行 `BindResource` 恢复旧 ID（CDN `BatchDeployCert` / WAF 域名替换 / ALB-NLB 监听更新）

**Expected Result**: 各产品资源证书引用恢复为旧证书，逐产品状态如实落账（每产品覆盖，不因某产品失败跳过呈报）

### Step 4: 回滚后状态收敛

**User Action**: 核对变更单/映射终态

**Expected Result**: 回滚后变更单终态如实（回滚成功/部分失败），映射不因回滚产生重复行；再次替换不受污染

## Edge Cases

### Step 2b: 回滚目标无效拒绝回滚

**Precondition**: 旧云证书已被云侧删除（GetCert 判定不在库/不可用）

**User Action**: 发起回滚

**Expected Result**: 回滚拒绝且不误绑（三判定拦截），呈报回滚目标无效的静态 reason，线上保持新绑定现状

### Step 2c: 无指纹通道映射回退兜底

**Precondition**: WAF/ALB/NLB 资源无指纹通道，且映射中无旧 ID 记录

**User Action**: 发起回滚

**Expected Result**: 映射回退无果时按既有占位/失败语义呈报（确定性占位指纹口径），不伪造有效性判定

### Step 3b: 某产品恢复失败

**Precondition**: 四产品回滚中某产品 BindResource 失败（如 CDN 域名已下线）

**User Action**: 执行回滚

**Expected Result**: 失败产品如实呈报静态 reason，其他产品照常恢复；失败项可重试且幂等（重跑同结果）

### Step 3c: 回滚重跑幂等

**Precondition**: 回滚已成功完成

**User Action**: 再次触发回滚

**Expected Result**: 幂等——不重复绑定、状态同结果，无重复映射行

### Step 4b: 验证窗口正常通过不回滚

**Precondition**: 拨测线上指纹 = 新证书指纹

**User Action**: 确认验证窗口通过并续批/终批

**Expected Result**: 不触发回滚，批次正常推进（回滚入口仅在异常时使用，语义不误触）

## Journey Invariants

- 回滚前必经 `GetCert` 目标有效性判定（三判定），无效目标不误绑
- WAF/ALB/NLB 无指纹通道不阻塞回滚判定——GetCert 映射回退兜底
- 回滚逐产品覆盖与呈报，失败可重试且幂等
- 验证窗口拨测云无关（ProbeDomains 按域名），回滚语义与五云同一状态机
- 回滚不产生重复映射行，不破坏后续替换能力

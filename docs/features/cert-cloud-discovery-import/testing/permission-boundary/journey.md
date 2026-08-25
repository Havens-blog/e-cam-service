---
feature: "cert-cloud-discovery-import"
journey: "permission-boundary"
risk_level: "Low"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-cloud-discovery-import/proposal.md
generated: "2026-08-25"
---

# Journey: permission-boundary

**Risk Level**: Low

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

发现导入全部端点（预览/快照状态/导入/进度）的权限边界：非 OpsEngineer 角色一律 403，OpsEngineer 正常访问，被拒请求无任何状态副作用。(Source: proposal.md Key Scenario "权限" + Success Criterion 8 + "托管形态"权限沿用说明)

## Setup

- 系统存在可用 done 快照与可导入条目（供授权角色对照验证）
- 具备两类测试身份：非 OpsEngineer 角色（如 viewer）已登录、OpsEngineer 角色已登录
- 发现预览/快照状态/导入/进度端点已按 RequireRoles(RoleOpsEngineer) 挂载

## Happy Path

### Step 1: 非 OpsEngineer 访问预览与快照状态端点

**User Action**: 以非 OpsEngineer 角色身份请求发现预览端点与快照状态查询端点

**Expected Result**: 两个端点均返回 403，响应不泄漏端点内部细节

### Step 2: 非 OpsEngineer 访问导入与进度端点

**User Action**: 以非 OpsEngineer 角色身份请求发现导入端点与会话进度端点

**Expected Result**: 返回 403，不创建任何导入会话，不触发任何云 API 调用

### Step 3: OpsEngineer 正常访问对照

**User Action**: 切换为 OpsEngineer 角色身份请求同一组端点

**Expected Result**: 预览与快照状态正常返回数据；导入端点正常创建会话（202 语义）；进度端点正常返回会话状态——证明 403 来自角色判定而非端点不可用

## Edge Cases

### Step 1b: 未认证请求

**Precondition**: 请求不携带有效登录会话（未认证）

**User Action**: 匿名请求发现预览/导入/进度端点

**Expected Result**: 返回 401 语义（认证失败先于角色判定），不进入任何业务逻辑

### Step 2b: 其它已登录低权角色请求导入端点

**Precondition**: 已登录但角色为任意非 OpsEngineer 角色（多角色矩阵覆盖）

**User Action**: 请求发现导入端点

**Expected Result**: 一律 403（权限矩阵单测覆盖全部非 OpsEngineer 角色）

### Step 3b: 被拒后无状态副作用

**Precondition**: 非 OpsEngineer 角色的导入请求已被 403 拒绝

**User Action**: 运维人员事后核对会话集合与台账数据

**Expected Result**: 不存在被拒请求创建的会话、台账记录、映射或引用回填——403 拒绝路径零状态副作用

## Journey Invariants

- 全部发现端点（预览/快照状态/导入/进度）恒定同一权限口径：RequireRoles(RoleOpsEngineer)
- 403 响应不泄漏端点内部细节与堆栈信息
- 被拒请求（401/403）不产生任何会话、台账、映射或云 API 副作用
- 权限判定先于任何业务处理与数据访问

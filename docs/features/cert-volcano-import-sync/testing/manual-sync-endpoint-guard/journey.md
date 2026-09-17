---
feature: "cert-volcano-import-sync"
journey: "manual-sync-endpoint-guard"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-volcano-import-sync/proposal.md
generated: "2026-09-17"
---

# Journey: manual-sync-endpoint-guard

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

手动触发入口（POST /api/v1/certs/discovery/sync，挂在既有 DiscoveryHandler，RequireRoles(RoleOpsEngineer)）的端点契约：OpsEngineer 受理 200 一次性摘要、running 冲突 409 语义、角色/认证边界、sessionId 复用既有进度轮询。(Source: proposal.md In Scope "手工触发入口" + Success Criteria 6；触发即产生状态变更故定级 High)

## Setup

- 端点已注册且角色中间件生效
- 操作者角色可配置：OpsEngineer / 其他角色 / 未认证
- 同步服务可运行（云侧以 fake/桩承载）

## Happy Path

### Step 1: OpsEngineer 手动触发同步

**User Action**: 具备 OpsEngineer 角色的运维人员经手动入口发起一轮多云证书同步

**Expected Result**: 受理并返回一次性 200 摘要（sessionId 与导入/跳过/补刷/漂移/失败计数），同步启动且 operator=manual

### Step 2: 凭 sessionId 轮询进度至终态

**User Action**: 客户端凭摘要中的 sessionId 复用既有进度轮询端点

**Expected Result**: sessionId 非空即可续看会话进度直至终态，不丢结果

### Step 3: 失败摘要核对

**User Action**: 存在失败条目时检查摘要失败面

**Expected Result**: 失败摘要仅白名单字段+静态 reason，无云侧错误细节

### Step 4: 空闲期重复手动触发

**User Action**: 前轮终态后再次手动触发

**Expected Result**: 再次 200 受理并启动新一轮，幂等收敛（无重复台账/映射）

## Edge Cases

### Step 1b: running 中触发冲突

**Precondition**: 已有一轮同步 running

**User Action**: 经手动入口再次触发

**Expected Result**: 即时 409 CERT_SYNC_IN_PROGRESS 结构化冲突（非 500），在途会话不受影响

### Step 1c: 非 OpsEngineer 已认证角色触发

**Precondition**: 会话已认证但角色为 viewer/auditor/ops_supervisor/仅能力码账号/未知显式 cert_role

**User Action**: 经手动入口触发

**Expected Result**: 一律 403（未知显式角色 deny 不降级 viewer）

### Step 1d: 未认证请求

**Precondition**: 请求无有效认证会话

**User Action**: 经手动入口触发

**Expected Result**: 401，无敏感信息泄露

### Step 1e: 已认证但无角色信号会话触发

**Precondition**: 已认证会话未声明任何 cert_role 信号

**User Action**: 经手动入口触发

**Expected Result**: 403（无有效角色即 deny）

### Step 2b: 冲突路径无轮询句柄

**Precondition**: 409 冲突路径（未创建新会话）

**User Action**: 客户端检查冲突响应

**Expected Result**: 无新 sessionId 产生，不引导轮询；在途会话进度经其既有 sessionId 继续可查

### Step 3b: 受理与执行结果解耦

**Precondition**: 触发受理成功但同步执行中出现失败条目

**User Action**: 核对摘要与会话终态

**Expected Result**: 受理成功不代表全部导入成功，失败在会话逐项结果/终态（partial_failed）中如实呈现

### Step 4b: 触发后服务端异常边界

**Precondition**: 同步服务依赖异常（未装配/内部错误）

**User Action**: 经手动入口触发

**Expected Result**: 结构化错误响应（非泄露堆栈），服务不 panic

## Journey Invariants

- 端点角色边界恒为 OpsEngineer 白名单：其他角色 403、未认证 401
- 冲突语义 409（CERT_SYNC_IN_PROGRESS）即时返回，不阻塞排队
- 摘要/冲突/错误响应不携带云侧错误细节与堆栈
- 手动轮与定时轮共享同一幂等收敛语义（不产生重复台账/映射）

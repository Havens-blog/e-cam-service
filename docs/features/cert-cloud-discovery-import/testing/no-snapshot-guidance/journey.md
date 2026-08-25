---
feature: "cert-cloud-discovery-import"
journey: "no-snapshot-guidance"
risk_level: "Medium"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-cloud-discovery-import/proposal.md
generated: "2026-08-25"
---

# Journey: no-snapshot-guidance

**Risk Level**: Medium

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

无任何扫描快照时，运维人员按前端引导完成"触发扫描 → 轮询快照状态 → done 后进入预览"的闭环，不依赖单次长请求同步返回。(Source: proposal.md Key Scenario "无扫描快照" + Success Criterion 3 + Scope "快照状态查询端点")

## Setup

- 台账页可访问，操作者具备 OpsEngineer 角色
- 系统当前不存在任何 status=done 的引用扫描快照
- 五云扫描通道可用（至少一个云账号可发起引用扫描）

## Happy Path

### Step 1: 请求预览得到无快照引导

**User Action**: 运维人员在无快照状态下请求云端发现预览

**Expected Result**: 预览端点返回 NO_SNAPSHOT 结构化错误码（非 500），前端展示"先执行扫描"引导而非报错死路

### Step 2: 按引导触发引用扫描

**User Action**: 运维人员点击引导中的触发扫描操作

**Expected Result**: 系统发起一次引用扫描，产生 running 状态的快照；引导切换为轮询等待态

### Step 3: 轮询快照状态直至终态

**User Action**: 前端按固定间隔轮询快照状态查询端点，运维人员等待状态从 running 推进

**Expected Result**: 每次轮询返回最近快照的 status/startedAt/partialFailures；running 期间持续轮询，不因单次长请求阻塞或被网关/浏览器超时打断

### Step 4: 快照 done 后进入预览

**User Action**: 轮询观测到快照状态变为 done，运维人员进入预览列表

**Expected Result**: 预览基于该 done 快照正常生成唯一证书清单，引导流程闭环，进入 first-ledger-import 的预览-确认流程

## Edge Cases

### Step 3b: 扫描失败展示部分失败明细

**Precondition**: 触发的扫描收敛到 failed 终态（而非 done）

**User Action**: 运维人员查看轮询结果

**Expected Result**: 前端展示快照状态端点返回的 partialFailures 明细，不进入预览，可再次发起扫描重试

### Step 3c: 他人已触发扫描在途

**Precondition**: 请求预览时最近快照状态为 running（扫描已被他人/前次操作触发，尚无 done 快照）

**User Action**: 运维人员请求预览

**Expected Result**: 引导直接进入轮询等待既有 running 快照，而非重复触发新扫描

### Step 1b: 最近快照为 failed 且无 done 快照

**Precondition**: 系统存在历史 failed 快照但从未有 done 快照

**User Action**: 运维人员请求云端发现预览

**Expected Result**: 仍返回 NO_SNAPSHOT 错误码（done 快照不存在），引导重扫；failed 快照的 partialFailures 可经快照状态端点查看

### Step 4b: 扫描部分成功仍有引用落库

**Precondition**: 快照收敛到 done 但部分云账号扫描失败（partialFailures 非空）

**User Action**: 运维人员进入预览列表

**Expected Result**: 预览基于已成功落库的引用生成清单；partialFailures 信息可查，运维人员可判断是否需要重扫补齐

## Journey Invariants

- 引导流程绝不依赖单次长请求同步返回扫描结果（多账号规模下由轮询承接，避免网关/浏览器超时打断）
- 快照状态查询端点只读最近快照，不改变扫描编排的同步至终态语义
- NO_SNAPSHOT 必须为结构化错误码（非 500），保证前端可编程识别并进入引导分支
- 快照状态端点与预览端点同权限口径（非 OpsEngineer 403）

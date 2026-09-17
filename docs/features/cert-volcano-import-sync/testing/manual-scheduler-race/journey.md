---
feature: "cert-volcano-import-sync"
journey: "manual-scheduler-race"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-volcano-import-sync/proposal.md
generated: "2026-09-17"
---

# Journey: manual-scheduler-race

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

手动轮与定时轮同刻竞态的防重与幂等收敛：调度面 CAS 防重不重启、手动面冲突即时 409、两轮重叠同指纹经 ErrDuplicateFingerprint 幂等消化（恰一条台账、双会话均 success 无 failed 条目）。(Source: proposal.md Key Scenario "手动与定时竞态" + Proposed Solution 冲突策略表"定时轮与手动轮同刻触发"行 + Success Criteria 3/4)

## Setup

- 同步服务共享同一 CAS 守卫（调度/手动同源）
- 两轮差异集重叠同一证书
- 手动触发入口可用

## Happy Path

### Step 1: 定时轮正常启动

**User Action**: 调度时钟到达 01:00 触发同步轮

**Expected Result**: CAS 占位成功，会话启动并标识 operator=scheduler

### Step 2: 空闲期手动触发同步

**User Action**: 运维人员经手动入口发起一轮同步

**Expected Result**: 受理并返回一次性 200 摘要（含 sessionId 与导入/跳过等计数），会话标识 operator=manual

### Step 3: 两轮处理重叠证书

**User Action**: 定时轮与手动轮（先后或竞态窗口内）各自处理同一指纹实例

**Expected Result**: 两轮各自进入导入路径，写入竞争被幂等语义接管

### Step 4: 后到者幂等消化

**User Action**: 后到者写入台账时捕获指纹唯一键冲突（ErrDuplicateFingerprint）

**Expected Result**: 转取既有证书补建映射，条目记 success（幂等语义，不算失败）

### Step 5: 收敛核对

**User Action**: 等待双会话终态，核对台账/映射/失败计数

**Expected Result**: 该指纹台账恰 1 条；双会话均无 failed 条目；映射唯一且指向正确

## Edge Cases

### Step 1b: running 中定时轮再触发

**Precondition**: 前一轮同步 running（CAS 未释放）

**User Action**: 调度点再次触发

**Expected Result**: CAS 拒绝，本轮静默跳过不重启不报错，在途轮不受影响

### Step 1c: running 中手动触发

**Precondition**: 前一轮同步 running（CAS 未释放）

**User Action**: 运维人员经手动入口触发

**Expected Result**: 手动入口不阻塞排队：即时 409 CERT_SYNC_IN_PROGRESS 结构化冲突（非 500），在途会话不受影响

### Step 1d: 前轮终态后 CAS 释放

**Precondition**: 前一轮已收敛终态

**User Action**: 再次触发（调度或手动）

**Expected Result**: 可正常启动新一轮（无死锁、无残留占位）

### Step 3b: 跨云同指纹同刻导入

**Precondition**: 两云内容相同（同指纹）证书在同窗口被两轮分别处理

**User Action**: 双轮处理两实例

**Expected Result**: 幂等收敛：台账恰 1 条，两云各自账号各建映射

### Step 4b: 双方均 success 无失败条目

**Precondition**: 竞态窗口内双写入完成

**User Action**: 核对双会话逐项结果

**Expected Result**: 双方条目均 success，失败计数为 0（幂等语义不降级失败）

### Step 5b: 手动摘要 sessionId 复用轮询

**Precondition**: 手动触发受理成功

**User Action**: 客户端凭摘要中的 sessionId 复用既有进度轮询端点

**Expected Result**: sessionId 非空即可续看进度至终态，不丢结果

### Step 5c: 冲突路径响应边界

**Precondition**: 409 冲突路径返回

**User Action**: 检查冲突响应体

**Expected Result**: 仅冲突语义（CERT_SYNC_IN_PROGRESS），无云侧错误细节、无堆栈

## Journey Invariants

- 台账指纹全局唯一：竞态双方不产生第二条同指纹台账
- 竞态撞指纹恒记 success，永不降级失败条目
- CAS 守卫保证任一时刻至多一轮同步执行（调度面静默跳过、手动面 409）
- 冲突/失败响应不携带云侧错误细节
- 幂等：重复轮结果收敛，不产生重复台账/映射

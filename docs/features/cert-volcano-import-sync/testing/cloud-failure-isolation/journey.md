---
feature: "cert-volcano-import-sync"
journey: "cloud-failure-isolation"
risk_level: "Medium"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-volcano-import-sync/proposal.md
generated: "2026-09-17"
---

# Journey: cloud-failure-isolation

**Risk Level**: Medium

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

云 API 失败/限流与空/失效账号场景的逐云逐账号隔离：单云单账号失败记 errorReason、会话终态 partial_failed、其余云正常完成，失败轮可重跑幂等收敛。(Source: proposal.md Key Scenarios "空/失效账号" + "云 API 失败/限流" + Success Criteria 6)

## Setup

- 六云×账号矩阵中某云某账号 API 失败/限流，另一账号无证书，其余正常
- 同步入口（定时或手动）可用

## Happy Path

### Step 1: 触发同步轮

**User Action**: 触发一轮同步（定时到达或手动入口）

**Expected Result**: 受理并创建同步会话，CAS 占位成功

### Step 2: 枚举六云 × active 账号

**User Action**: 同步服务枚举全部证书可达云与 active 账号

**Expected Result**: 逐云逐账号构成独立处理单元，互不共享失败状态

### Step 3: 单云单账号 List 失败

**User Action**: 处理云 API 失败/限流的云×账号单元

**Expected Result**: 该云该账号隔离失败，errorReason 记录（静态 reason），不中断其他单元

### Step 4: 其余云正常完成

**User Action**: 同步继续处理其余云×账号

**Expected Result**: 正常单元的导入/跳过/补刷照常收敛，不受失败单元影响

### Step 5: 会话终态 partial_failed 与重跑收敛

**User Action**: 等待会话终态，核对失败摘要后重跑一轮

**Expected Result**: 终态 partial_failed，失败摘要仅白名单字段+静态 reason；重跑仅处理剩余失败面并幂等收敛

## Edge Cases

### Step 1b: 失败轮结束后守卫释放

**Precondition**: 前一轮以 partial_failed 终态结束

**User Action**: 再次触发同步

**Expected Result**: CAS 正常释放，新一轮可启动（失败终态不残留占位）

### Step 2b: 空账号无证书

**Precondition**: 某云某 active 账号下无任何证书实例

**User Action**: 同步枚举该账号

**Expected Result**: 空枚举按跳过处理，不计失败

### Step 2c: 失效/不可达账号

**Precondition**: 某账号凭证失效或网络不可达

**User Action**: 同步枚举该账号

**Expected Result**: 该云该账号跳过/隔离失败，不中断其他云（账号维度隔离）

### Step 3b: 限流语义

**Precondition**: 云 API 返回限流类错误

**User Action**: 处理该单元

**Expected Result**: 静态 reason 记录，不做重试风暴（单轮不反复重试同一单元）

### Step 3c: 云侧错误细节不进响应

**Precondition**: 任一云单元失败

**User Action**: 检查会话/摘要中的失败信息

**Expected Result**: 云侧错误细节不进响应仅日志，errorReason 为白名单静态文案

### Step 4b: 失败云证书在重跑轮补齐

**Precondition**: 上轮某云失败，云端证书仍在

**User Action**: 重跑一轮同步

**Expected Result**: 幂等可重入：失败面实例本轮入账，成功面不重复处理

### Step 5b: 终态判定语义

**Precondition**: 分别构造全成功与含失败两轮

**User Action**: 核对终态

**Expected Result**: 无失败=completed；任一失败=partial_failed；不出现中间态卡死

## Journey Invariants

- 单云单账号失败不中断其他云与账号
- errorReason 为白名单静态文案，不携带云响应片段/凭证
- 会话终态二值收敛：completed / partial_failed
- 失败可重跑幂等收敛
- 云凭证仅内存传递禁入日志

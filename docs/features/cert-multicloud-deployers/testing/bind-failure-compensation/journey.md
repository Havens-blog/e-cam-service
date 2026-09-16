---
feature: "cert-multicloud-deployers"
journey: "bind-failure-compensation"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-multicloud-deployers/proposal.md
generated: "2026-09-16"
---

# Journey: bind-failure-compensation

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

变更执行中绑定段失败后，运维人员依赖既有补偿链路收敛三云半成品状态：绑定失败显式报错 → CleanupOrphan 补偿删除已上传云证书 → 映射 active→orphan 入清理队列 → 清理队列删除云侧孤儿证书，全程复用 aliyun/tencent 同一补偿状态机，不残留悬空的云证书资源。(Source: proposal.md Key Scenario "失败模式" + Non-Functional Requirements "一致性" + Success Criterion 4)

## Setup

- 三云部署器已装配，变更清单已确认并进入分批执行
- 目标条目已完成上传段（云证书库中已存在新上传的证书，云证书 ID 已产生或即将落映射）
- 清理队列与 CloudCertMapping 既有机制可用（与 aliyun/tencent 共用）

## Happy Path

### Step 1: 绑定段执行失败并显式报错

**User Action**: 系统对三云某条目执行绑定段（如云 API 拒绝、目标资源状态不允许），运维人员查看执行结果

**Expected Result**: 变更条目进入失败态，失败原因明确（云端错误细节仅入日志不进响应）；该条目不进入验证窗口、不标记成功

### Step 2: 触发 CleanupOrphan 补偿

**User Action**: 系统按两段式编排对失败条目执行 CleanupOrphan 补偿，运维人员确认补偿结果

**Expected Result**: 已上传到云证书库的新证书被删除（华为 SCM / AWS ACM / Azure KV 按各自删除 API），补偿结果幂等可重入

### Step 3: 映射状态迁移 active→orphan 入清理队列

**User Action**: 系统将该条目的 CloudCertMapping 记录由 active 迁移为 orphan 并加入清理队列

**Expected Result**: 映射记录状态为 orphan，进入清理队列等待删除；状态机与 aliyun/tencent 失败路径同构

### Step 4: 清理队列执行删除云侧孤儿证书

**User Action**: 运维人员等待/触发清理队列消费，删除云侧孤儿证书

**Expected Result**: 云侧孤儿证书被删除，映射与队列状态收敛一致，不留半成品资源；变更单整体保持失败可重跑语义

## Edge Cases

### Step 1b: 上传成功但绑定前进程中断

**Precondition**: UploadCert 已返回云证书 ID，进程在 BindResource 执行前崩溃/重启

**User Action**: 运维人员在恢复后查看该条目状态

**Expected Result**: 悬空的上传证书可经补偿链路收敛（orphan 入清理队列），重跑时重新走两段式，不复用悬空 ID 直接绑定

### Step 2b: CleanupOrphan 重复调用幂等

**Precondition**: 同一条目的补偿被触发两次（重试与队列消费竞争）

**User Action**: 系统第二次执行 CleanupOrphan

**Expected Result**: 二次调用不报错、不重复删除云侧资源，结果与单次调用一致（幂等对齐既有 CleanupOrphan 语义）

### Step 2c: 绑定失败且云证书 ID 未落映射

**Precondition**: 绑定失败发生时映射记录尚未写入（上传结果仅存在于执行上下文）

**User Action**: 系统执行补偿

**Expected Result**: 仍按上传结果补偿删除云侧证书，不产生脏映射记录；补偿不依赖映射先落库

### Step 3b: 补偿自身失败（云侧删除失败）

**Precondition**: CleanupOrphan 调用云侧删除 API 失败（限流/网络）

**User Action**: 系统记录补偿失败并重试

**Expected Result**: 映射保留 orphan 状态并留在清理队列重试，不吞错、不误标已清理；最终收敛或显式暴露未清理项

### Step 4b: 失败后重跑整个变更单

**Precondition**: 条目补偿完成（orphan 已清理），运维人员重跑变更单

**User Action**: 运维人员对失败批次重新发起执行

**Expected Result**: 重跑重新上传/绑定产生新的云证书 ID 与映射，不复用已清理的 orphan 记录；新旧映射不冲突

### Step 4c: 清理期间云侧证书已不存在

**Precondition**: 清理队列消费时云侧证书已被人工删除

**User Action**: 系统执行清理删除

**Expected Result**: 判定"已不存在"为清理成功语义，不报错不重试死循环，队列状态收敛

## Journey Invariants

- 绑定失败必经 CleanupOrphan 补偿，且补偿幂等：任何重试/并发路径不重复删除云资源
- 失败状态、orphan 迁移、清理队列与 aliyun/tencent 走同一状态机，三云不新增补偿机制
- 云端错误细节仅入日志不进 API 响应；失败原因对用户呈现为静态文案
- 映射状态迁移单调可追溯（active→orphan），不存在 active 与 orphan 并存的同一云证书 ID 记录
- 清理队列最终收敛：每个 orphan 记录要么被删除、要么显式保留待重试，不允许静默丢失

---
feature: "disk-ops-insight"
journey: "disk-day-gate-resilience"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/disk-ops-insight/proposal.md
generated: "2026-09-21"
---

# Journey: disk-day-gate-resilience

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

Disk 每日采集接入 `scheduler_state` 持久化日闸(disk 键)后的韧性旅程:原子认领、提交、重启一致性、多资源并行复用,以及写失败退避重试+告警、读失败 5 分钟退避、特性开关回滚三条故障路径。该 Journey 验证提案 Success Criterion 4(「重启×3 各仅 1 条;特性开关切回内存闸后 NAS/CDN/OSS/Disk 调度仍可用」)与 Key Risks 中「日闸 disk 键接入破坏既有键 L/H」的回归保障。

## Setup

- `scheduler_state` 持久化日闸机制可用(NAS/OSS 已上线验证的 T7 实现),特性开关 `SCHEDULER_PERSISTENT_GATE_ENABLED` 开启
- nas/cdn/oss 三既有资源键已有历史状态记录
- SchedulerGateAlerter 告警桥已接入

## Happy Path

### Step 1: 持久化日闸原子认领

**User Action**: 每日调度触发 disk 采集,执行器对 scheduler_state disk 键发起当日原子认领。

**Expected Result**: 认领结果原子确定:当日未被认领则本执行器胜出并开始采集;已被认领则本执行器跳过。

### Step 2: 采集成功后提交日闸

**User Action**: 采集与落库完成后,执行器提交当日日闸状态。

**Expected Result**: 日闸记录当日已完成状态;当日后续调度触发不再重复采集。

### Step 3: 服务重启后状态一致

**User Action**: 重启服务(模拟 ×3),观察每次重启后的调度行为。

**Expected Result**: 每次重启后当日采集各仅触发 1 条(共 1 次成功执行),持久化状态防止重复采集,也不丢当日认领。

### Step 4: 四资源日闸并行复用

**User Action**: 观察 CDN/NAS/OSS/Disk 四资源的每日调度在共享日闸机制下并行运行。

**Expected Result**: 各资源按各自 resource_type 键独立认领/提交,互不干扰;Disk 分支接入后 NAS/CDN/OSS 既有调度行为不变。

## Edge Cases

### Step 1b: 日闸写失败

**Precondition**: 认领/提交时 scheduler_state 写入失败(存储抖动/超时)。

**User Action**: 执行器处理写失败。

**Expected Result**: 指数退避重试;重试仍失败则升级告警(SchedulerGateAlerter);不静默丢失当日认领,也不在未认领成功时执行采集。

### Step 2b: 日闸读失败

**Precondition**: 读取 scheduler_state 状态时存储不可用。

**User Action**: 执行器读取日闸状态。

**Expected Result**: 进入 ≥5 分钟退避,不热循环刷存储;恢复后按正常语义继续认领。

### Step 3a: 特性开关回滚到内存闸

**Precondition**: 运维关闭 `SCHEDULER_PERSISTENT_GATE_ENABLED`(回滚场景)。

**User Action**: 观察回滚后的调度行为。

**Expected Result**: 调度切换回内存闸兜底,NAS/CDN/OSS/Disk 四资源调度仍可用;不因回滚产生重复采集或调度停摆。

### Step 4a: 既有资源键行为回归

**Precondition**: disk 键首次接入 scheduler_state,既有 nas/cdn/oss 键已有历史记录。

**User Action**: 回归验证三既有键的认领/提交/重启语义。

**Expected Result**: nas/cdn/oss 键行为与 Disk 接入前完全一致(按 resource_type 分键,无键冲突、无状态串扰)。

### Step 4b: 并发认领竞争

**Precondition**: 多个 goroutine/实例同时发起当日 disk 键认领。

**User Action**: 并发触发认领。

**Expected Result**: 原子认领保证恰好一个胜出,其余全部跳过;不存在双执行或全跳过(死锁)。

## Journey Invariants

- 原子性:任意并发度下,同一 resource_type 键同一日的认领结果恰好一个胜出
- 持久化一致性:服务重启不改变「当日已认领/已完成」事实,重启 ×3 各仅 1 条
- 故障不静默:写失败/读失败必须走退避与告警路径,禁止热循环与静默吞错
- 既有键零回归:disk 键的接入与回滚不得改变 nas/cdn/oss 键的任何既有行为

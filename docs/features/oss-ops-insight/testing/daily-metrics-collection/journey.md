---
feature: "oss-ops-insight"
journey: "daily-metrics-collection"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/oss-ops-insight/proposal.md
generated: "2026-09-20"
---

# Journey: daily-metrics-collection

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

每日调度自动采集 OSS 容量/对象数指标：持久化日闸 oss 键原子认领当日 → 按活跃账号遍历 OSS bucket 调厂商监控 API → 指标行落库（同日首写生效、次日补采覆盖昨日）。核心保障是「服务重启当日不重复提交」与「oss 键接入不破坏既有 nas/cdn 键行为」（Key Scenario 1、Key Scenario 3、SC-2/SC-4）。

## Setup

- `scheduler_state` collection 可用，特性开关 `SCHEDULER_PERSISTENT_GATE_ENABLED` 默认开启
- 至少 1 个活跃账号（已纳管且存在 ≥1 个 OSS bucket）注册在租户下
- 调度循环已启动，当前时间在 00:10 之后（采集区间 `[昨日, 今日]`）
- nas/cdn 日闸键已存在既有记录（用于验证分键互不干扰）

## Happy Path

### Step 1: oss 日闸原子认领当日

**User Action**: 调度器触发 OSS 指标采集；对 `scheduler_state` 执行一次 `findOneAndUpdate`（条件 `resource_type=oss AND last_date<today`，更新为 today）。

**Expected Result**: 认领成功（返回当日认领权），认领成功才提交 `oss:collect_metrics` 采集任务；oss/nas/cdn 按资源类型分键互不干扰。

### Step 2: 按活跃账号遍历 bucket 采集

**User Action**: 执行器遍历活跃账号下的 OSS bucket，适配器调用厂商监控 API，采集区间取 `[昨日, 今日]`（补昨日完整行 + 今日初态）。

**Expected Result**: 每个 bucket 产出昨日与今日两组指标值；账号级互斥 + bucket 有界并发（复用 nasAccountGate 模式）；单厂商/单 bucket 失败只影响自身。

### Step 3: 指标行落库（首写生效 + 补采覆盖）

**User Action**: 采集结果按唯一键 `(account_id, bucket_name, date)` upsert 写入 `ecam_oss_metric`：今日行首写生效（已有则不覆盖），昨日行覆盖更新为厂商当日最终聚合值。

**Expected Result**: 昨日行从 00:10 初态被更新为日末态快照并随后冻结；今日行保持首次写入值；跨账号同 bucket 名各留一行。

### Step 4: 服务重启验证当日不重复提交

**User Action**: 连续 3 次重启服务进程，每次观察调度器当日提交的 OSS/NAS/CDN 采集任务数。

**Expected Result**: 每次重启后 OSS/NAS/CDN 任务各仅 1 条（当日已认领，重启不再重复提交）。

## Edge Cases

### Step 1b: 多副本同时竞争认领

**Precondition**: 多副本部署，两个实例同日同时触发 oss 日闸认领。

**User Action**: 两个实例并发执行 `findOneAndUpdate` 认领。

**Expected Result**: 仅一个实例认领成功并提交任务，另一个条件不匹配认领失败、不提交。

### Step 1c: 手动触发与自动调度重叠

**Precondition**: 运营当日手动执行 `oss:collect_metrics`，与每日自动任务同日重叠。

**User Action**: 手动任务尝试认领当日。

**Expected Result**: 同一资源当日只被一个任务认领，不产生重复采集任务。

### Step 1d: 日闸写失败

**Precondition**: 故障注入——模拟写 oss 日闸失败 3 次。

**User Action**: 认领时写 `scheduler_state` 失败。

**Expected Result**: 指数退避重试并升级告警触发；退避期间不洪泛任务队列；恢复后当日正常认领一次。

### Step 1e: 日闸读失败

**Precondition**: 故障注入——模拟读 oss 日闸状态失败。

**User Action**: 调度循环读取日闸状态。

**Expected Result**: 设置 ≥5 分钟最短退避窗口再重读，退避窗口内不重复提交任务，不挂在分钟级调度循环上逐分钟洪泛。

### Step 1f: 首部署无 oss 日闸记录

**Precondition**: 首部署时 `scheduler_state` 尚无 oss 记录，`last_date` 为空。

**User Action**: 调度器首次触发。

**Expected Result**: 视为首次认领，认领后触发一次当日提交（不回溯补采历史），此后按每日一次语义运行。

### Step 1g: 特性开关切回内存闸（回滚验证）

**Precondition**: mongo 日闸出现不可恢复故障，运维将 `SCHEDULER_PERSISTENT_GATE_ENABLED` 切回内存闸。

**User Action**: 切换开关后观察 OSS/NAS/CDN 调度任务提交。

**Expected Result**: 三类调度任务仍正常提交、指标采集不中断（接受重启重复提交旧缺陷换取调度器可用），回滚原因与重新开启计划被记录。

### Step 3b: 同日重跑不覆盖今日行

**Precondition**: 今日行已存在（首写已完成），执行器因回填/手动触发再次写入同日值。

**User Action**: upsert 同 `(account_id, bucket_name, 今日)` 行。

**Expected Result**: 今日行保持首写值不被覆盖（首写生效仅保护今日行），仅补当日缺失行。

### Step 3c: 昨日行补采后冻结

**Precondition**: 昨日行已在次日补采中覆盖更新为日末态。

**User Action**: 当日晚些时候再次触发包含昨日区间的写入。

**Expected Result**: 昨日行跨日不可变，重采不再触碰；冻结窗口以 Asia/Shanghai 自然日为准。

## Journey Invariants

- 当日（Asia/Shanghai 自然日）oss 资源类型的采集任务至多提交一次，无论重启、多副本、手动/自动重叠
- 认领成功是提交采集任务的唯一前提；日闸写失败必须走退避重试+告警，绝不静默跳过
- oss 键与 nas/cdn 键按 resource_type 分键：接入 oss 分支后 nas/cdn 的认领行为与既有记录不受影响
- 今日行当日内可变（首写生效）；昨日行次日补采后冻结不可变；两窗口语义不互相渗透
- 回滚开关切换不中断采集链路（降级为旧行为而非停摆），NAS/CDN/OSS 调度同时保持可用

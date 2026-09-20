---
feature: "oss-ops-insight"
journey: "vendor-failure-observability"
risk_level: "Medium"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/oss-ops-insight/proposal.md
generated: "2026-09-20"
---

# Journey: vendor-failure-observability

**Risk Level**: Medium

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

厂商适配器失败的可观测性路径：「探测不支持」与「调用失败」三分归类（INFO+空返回 vs ERROR+failures 计数）、失败不阻塞其他厂商、失败汇总入 `Result["failures"]`、必达厂商近 3 天零成功写库触发健康告警（Key Scenario 4、Non-Functional Requirements 可观测性项）。

## Setup

- 采集执行器已注册，至少覆盖一个必达厂商（aliyun/huawei/aws）与一个尽力而为厂商（tencent/volcengine）
- 租户下至少 2 个活跃账号、每账号 ≥1 个 OSS bucket
- 健康告警通道（必达厂商近 3 天零成功写库告警）已接通

## Happy Path

### Step 1: 必达厂商适配器调用失败不阻塞全流程

**User Action**: 故障注入使某必达厂商（如 aliyun）监控 API 调用失败，执行器完成一轮全量采集。

**Expected Result**: 该适配器只返回自身空；ERROR 日志 + error 字段记录；其他厂商与其他账号的采集正常继续，全流程不中断。

### Step 2: 尽力而为厂商探测不 supported 归类为非失败

**User Action**: 故障场景设为尽力而为厂商（如 tencent）实盘无 OSS 容量指标（探测不支持），执行器采集该厂商 bucket。

**Expected Result**: INFO 日志 + 空返回，不算失败、不进 failures 计数；与「调用失败」明确区分。

### Step 3: 失败汇总入 Result["failures"]

**User Action**: 一轮采集结束后查看执行器 Result 的 failures 汇总。

**Expected Result**: `Result["failures"]` 含 provider/account/error_count/last_error 维度，可定位到具体厂商与账号的失败明细。

### Step 4: 必达厂商持续失败触发健康告警

**User Action**: 故障注入使某必达厂商连续 3 天零成功写库，观察告警通道。

**Expected Result**: 健康告警触发，明确指出厂商与零成功时间窗；尽力而为厂商的失败不触发该健康告警。

## Edge Cases

### Step 1b: 单账号失败不阻塞同厂商其他账号

**Precondition**: 同一厂商下 account A 的凭证失效、account B 正常。

**User Action**: 执行器按账号级互斥遍历采集。

**Expected Result**: 仅 account A 计入失败，account B 正常落库；账号级失败互不传染。

### Step 2b: 失败后恢复成功

**Precondition**: 厂商 API 短暂故障后恢复可用。

**User Action**: 下一轮采集执行。

**Expected Result**: 恢复后正常落库，failures 计数按轮次归位（不无限累计历史失败），指标不丢日（缺失日由补采/data_status 覆盖）。

### Step 4b: 告警不洪泛

**Precondition**: 必达厂商持续零成功超过告警阈值，告警条件在多轮采集中持续满足。

**User Action**: 连续多轮采集观察告警发送。

**Expected Result**: 告警按合理节流策略发送，不逐轮洪泛重复告警；恢复成功后告警状态解除。

### Step 4c: 探测归因记录（订阅未开通 vs 指标不存在）

**Precondition**: 尽力而为厂商指标不可用，根因可能是「指标订阅未开通」或「指标不存在」。

**User Action**: 查看探测/失败归因记录。

**Expected Result**: 归因被显式记录（订阅未开通记「二期补+开通路径」），不伪装数据、不静默吞掉原因。

## Journey Invariants

- 任一厂商/账号的失败只影响自身，绝不阻塞其他厂商/账号/bucket 的采集继续
- 「探测不支持」（INFO+空返回）与「调用失败」（ERROR+failures 计数）两类路径严格区分，不混淆失败口径
- 所有适配器失败必须可观测：error 字段或 Result["failures"] 至少其一可见，绝不静默吞错
- 健康告警只针对必达厂商的持续零成功；尽力而为厂商的已知限制不告警

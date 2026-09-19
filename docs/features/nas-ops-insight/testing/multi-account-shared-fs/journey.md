---
feature: "nas-ops-insight"
journey: "multi-account-shared-fs"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/nas-ops-insight/proposal.md
generated: "2026-09-19"
---

# Journey: multi-account-shared-fs

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

多账号同 fs_id 并存（多活/共享实例）场景下的指标采集与隔离（Key Scenario 5、SC-5）：唯一键 `(account_id, fs_id, date)` 保证各账号独立保留，复用 CDN 多账号键经验（CDN 的「域名只归属一个账号」前提在 NAS 不成立）。upsert 唯一键冲突处理不当会导致跨账号数据互相覆盖（数据丢失风险），定级 High。

## Setup

- 同一物理 NAS 文件系统（相同 fs_id）被 ≥3 个云账号同时纳管（多活/共享实例）
- 三个账号均属活跃账号集合，采集可触达
- `ecam_nas_metric` 唯一索引 `(account_id, fs_id, date)` 已建立

## Happy Path

### Step 1: 三账号同 fs 并发采集

**User Action**: 触发采集，执行器分别以三个账号身份调用厂商监控 API 采集同一 fs_id 的容量/用量。

**Expected Result**: 每个账号产出各自指标值；任一账号采集失败不影响其余账号。

### Step 2: 唯一键各行独立落库

**User Action**: 采集结果按 `(account_id, fs_id, date)` upsert 写入。

**Expected Result**: 三个账号同日各留一行，互不覆盖、互不合并；跨账号同 fs 并存语义成立（≥3 账号同 fs 各留一行）。

### Step 3: 趋势按账号隔离读取

**User Action**: 分别以三个 account_id 请求同一 fs_id 的趋势接口。

**Expected Result**: 各账号返回各自视角的日值序列（趋势按账号保留各自行），鉴权校验各自通过。

### Step 4: 聚合层去重消费

**User Action**: 查看运营卡/Top。

**Expected Result**: 聚合按 fs_id 去重：取「日期 desc 再容量 desc」第一行代表该物理 fs，不双计、不跨账号求和/平均。

## Edge Cases

### Step 2b: 同账号同日重复写入

**Precondition**: 同一账号同日因手动/自动重叠被采集两次。

**User Action**: 第二次 upsert 同 `(account_id, fs_id, 今日)`。

**Expected Result**: 今日行首写生效，第二次写入不覆盖首写值；仅补缺失行。

### Step 2c: 次日补采跨账号覆盖隔离

**Precondition**: 三账号昨日行均已落库，次日补采按账号逐个执行。

**User Action**: 次日补采覆盖昨日行。

**Expected Result**: 每账号仅覆盖自己的昨日行；补采完成后昨日行冻结，跨账号互不触碰。

### Step 2d: 今日行当日修正

**Precondition**: 今日行已首写（00:10 初态），当日内获得更准确的厂商聚合值。

**User Action**: 当日内再次写入该账号今日行。

**Expected Result**: 首写生效——今日行不被修正（「首写生效」仅保护今日行，修正发生在次日补采覆盖环节）。

### Step 3b: 跨账号越权读取

**Precondition**: 运营 A 仅持有账号 1 的租户权限，尝试以账号 2 的 account_id 读同 fs 趋势。

**User Action**: 请求趋势接口。

**Expected Result**: 404，不泄露账号 2 的存在性与指标数据。

### Step 4b: 共享 fs 口径差异声明

**Precondition**: 共享 fs 在不同账号下的容量口径不同（共享配额/共享文件系统视图）。

**User Action**: 查看聚合视图该 fs 的容量值。

**Expected Result**: 以最新日期行的厂商返回值为该物理 fs 的容量/用量口径（代表物理文件系统容量），不做跨账号求和或平均，避免「双计已除、口径仍高估」。

## Journey Invariants

- 唯一键 `(account_id, fs_id, date)` 之下，任何写入路径都不得让一个账号的行覆盖/删除另一账号的行
- 趋势读取按账号隔离，聚合视图按 fs_id 去重——两条路径的隔离/去重语义互不混淆
- 共享 fs 的物理容量口径唯一来源 = 「日期 desc 再容量 desc」第一行的厂商返回值
- 首写生效仅作用今日行；昨日行冻结后任何账号/任何路径均不可变

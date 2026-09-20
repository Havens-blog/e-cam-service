---
feature: "oss-ops-insight"
journey: "multi-account-shared-bucket"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/oss-ops-insight/proposal.md
generated: "2026-09-20"
---

# Journey: multi-account-shared-bucket

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

同一 bucket_name 出现在多个云账号（跨账号共享存储）时的隔离与聚合正确性：唯一键 `(account_id, bucket_name, date)` 隔离落库各账号各留一行，Top 按 bucket_name 去重取代表行（日期 desc 再容量 desc），不跨账号求和/双计（Key Scenario 2、SC-2、SC-5）。

## Setup

- 租户下 3 个账号（account A/B/C）各自纳管了同名 bucket `shared-assets`（跨账号共享存储场景）
- `ecam_oss_metric` collection 已建立唯一键 `(account_id, bucket_name, date)`
- 采集执行器已配置且必达厂商适配器可用

## Happy Path

### Step 1: 三账号同名 bucket 同日各留一行

**User Action**: 执行器采集三账号下的 `shared-assets` bucket，结果按唯一键 `(account_id, bucket_name, date)` upsert 落库。

**Expected Result**: 同日 `shared-assets` 在 account A/B/C 下各有一行，互不覆盖、互不合并；容量/对象数各自反映各账号视角的真实值。

### Step 2: 同日幂等（首写生效）

**User Action**: 同日再次触发采集，对同 `(account_id, bucket_name, date)` 键重复 upsert。

**Expected Result**: 各账号行保持首写值不被覆盖（首写生效仅保护今日行），仅补当日缺失行。

### Step 3: 次日补采覆盖昨日行

**User Action**: 次日采集包含昨日区间，对昨日行执行覆盖更新。

**Expected Result**: 各账号的昨日行被更新为厂商当日最终聚合值后冻结，跨日不可变。

### Step 4: Top 按 bucket_name 去重取代表行

**User Action**: 用户请求账号视角 Top 列表，返回结果按 bucket_name 去重，代表行按「日期 desc 再容量 desc」选取。

**Expected Result**: `shared-assets` 在 Top 中只出现一次（代表行来自其所属账号的最新/最大容量行），不跨账号求和、不双计。

### Step 5: 趋势读取按账号精确隔离

**User Action**: 用户分别以 account A 与 account B 查询 `shared-assets` 的单 bucket 趋势。

**Expected Result**: 各自返回本账号的指标序列，不出现跨账号串数据；两条趋势曲线独立。

## Edge Cases

### Step 1b: 并发写同名键

**Precondition**: 两个采集 goroutine 并发写同一 `(account_id, bucket_name, date)` 键。

**User Action**: 并发 upsert 同一行。

**Expected Result**: 唯一键约束保证至多一行生效（首写生效），不产生重复行或脏数据。

### Step 2b: 跨账号共享 bucket 被误聚合（回归守护）

**Precondition**: 聚合逻辑误将跨账号同名 bucket 的容量求和。

**User Action**: 请求 Top/运营卡汇总。

**Expected Result**: 禁止跨账号求和/双计——总容量按唯一键隔离行各自统计，同名 bucket 不叠加多账号数值。

### Step 4b: 代表行选取的次序稳定性

**Precondition**: 同一 bucket_name 在多个账号有不同日期/容量的行。

**User Action**: 请求 Top 列表。

**Expected Result**: 代表行按「日期 desc 再容量 desc」确定性选取，多次请求结果稳定一致。

### Step 4c: 一账号有数据另一账号空

**Precondition**: `shared-assets` 在 account A 有指标行，account B 的采集为零/失败（仅 A 有数据）。

**User Action**: 请求 Top 列表与账号视角统计。

**Expected Result**: 代表行取自有数据的账号行；B 的缺失不把 bucket 从 Top 中抹除，也不把 B 的空值计入。

### Step 5b: 越权读取他人账号的同名 bucket

**Precondition**: 租户 A 的用户以 account B 的 account_id 查询 `shared-assets` 趋势（B 不属于租户 A）。

**User Action**: 请求单 bucket 趋势接口并携带越权 account_id。

**Expected Result**: 返回 404（不泄露账号存在性），不返回 B 的任何指标数据。

## Journey Invariants

- 唯一键 `(account_id, bucket_name, date)` 全程有效：同键至多一行，跨账号同名 bucket 各留一行、永不合并
- Top/聚合视图按 bucket_name 去重取代表行（日期 desc 再容量 desc），绝不跨账号求和/双计
- 今日行首写生效、昨日行补采后冻结，两窗口语义不互相渗透
- 租户隔离贯穿读取侧：越权 account_id 一律 404 且不泄露账号存在性

---
feature: "oss-ops-insight"
journey: "top-overview-insight"
risk_level: "Low"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/oss-ops-insight/proposal.md
generated: "2026-09-20"
---

# Journey: top-overview-insight

**Risk Level**: Low

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

账号视角的 OSS 存储规模 Top 排名与运营卡总览：Top 读取（`sort` ∈ storage_size|object_count，近 N 天均值口径，top 默认 10 最大 50，分页）、bucket_name 去重代表行、租户校验（越权 404 不泄露账号存在性）、运营卡总容量/对象数/近 7 天增速（SC-5、SC-6）。

## Setup

- 租户下有 ≥2 个账号、合计 ≥15 个有指标数据的 OSS bucket（可触发分页与 top 上限）
- 用户已通过鉴权（合法 tenantID 与凭证）
- 指标数据已落库多日（可计算近 N 天均值与近 7 天增速）

## Happy Path

### Step 1: 查看账号视角 Top 排名

**User Action**: 用户请求账号视角的 OSS Top 列表（默认 `sort=storage_size`，`top=10`）。

**Expected Result**: 返回按近 N 天均值口径排序的 Top bucket 列表，bucket_name 去重（同名跨账号 bucket 只出现一次，代表行按日期 desc 再容量 desc 选取）。

### Step 2: 切换排序维度为对象数

**User Action**: 用户以 `sort=object_count` 请求 Top 列表。

**Expected Result**: 返回按对象数近 N 天均值口径排序的 Top 列表；排序口径与容量排序一致（均值语义）。

### Step 3: 分页浏览大结果集

**User Action**: 用户以 `page=2&page_size=10` 请求 Top 列表第二页。

**Expected Result**: 返回第二页数据及分页元信息（total/page/page_size），翻页结果与第一页无缝衔接、不重不漏。

### Step 4: 查看运营卡总览

**User Action**: 用户打开 OSS 列表页查看顶部运营卡的总容量/对象数/近 7 天增速。

**Expected Result**: 运营卡数值来自指标表聚合，增速基于近 7 天窗口计算；数据齐备时正常展示。

## Edge Cases

### Step 1b: 越权 account_id 返回 404

**Precondition**: 租户 A 的用户以不属于本租户的 account_id 请求 Top。

**User Action**: 请求 Top 接口并携带越权 account_id。

**Expected Result**: 返回 404（不泄露账号存在性），不返回该账号的任何指标数据。

### Step 1c: 未授权访问

**Precondition**: 请求未携带有效凭证。

**User Action**: 无凭证请求 Top 接口。

**Expected Result**: 返回 401 Unauthorized，响应体含认证错误信息，不泄露敏感数据。

### Step 1d: sort 传非法值

**Precondition**: 客户端传入 `sort=cost`（不在 storage_size|object_count 枚举内）。

**User Action**: 请求 Top 接口。

**Expected Result**: 参数校验拒绝（400 类响应），明确提示合法 sort 枚举。

### Step 2b: top 超上限

**Precondition**: 客户端传入 `top=100`（超过最大 50）。

**User Action**: 请求 Top 接口。

**Expected Result**: 参数校验拒绝或按上限收敛（默认 10 最大 50 语义生效），不返回未受限结果集。

### Step 3b: 页码超界

**Precondition**: 结果共 3 页，客户端请求 `page=99`。

**User Action**: 请求 Top 接口。

**Expected Result**: 返回空数据页与正确分页元信息（total 不变），不报 500、不重复返回已有页数据。

### Step 4b: 无数据账号的运营卡空态

**Precondition**: 账号下尚无任何 OSS 指标行（未采集/采集失败）。

**User Action**: 查看该账号视角的运营卡。

**Expected Result**: 运营卡空态区分「无数据」与「采集失败」；采集异常时显示警示而非纯空，不显示 0 冒充数据。

## Journey Invariants

- Top/聚合一律以 `ecam_oss_metric` 指标表为唯一数据来源；排序口径统一为「近 N 天均值」
- bucket_name 跨账号同名去重取代表行（日期 desc 再容量 desc），绝不跨账号求和/双计
- 租户校验贯穿：account_id ∈ 当前租户账号集合，越权一律 404 且不泄露账号存在性
- `top` 默认 10 最大 50、`days` 限 1~90，参数边界在服务端强制

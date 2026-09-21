---
feature: "disk-ops-insight"
journey: "disk-metrics-query"
risk_level: "Low"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/disk-ops-insight/proposal.md
generated: "2026-09-21"
---

# Journey: disk-metrics-query

**Risk Level**: Low

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

Disk 指标读取接口的用户旅程:租户用户查询单盘趋势(`GET /assets/disk/metrics`)与账号视角 Top(`GET /assets/disk/top`),获得「最新一天」与「近 N 天均值」两类值,并能通过 qc_status → data_status 闭环分辨「使用率 0 是异常」。纯只读旅程,重点在租户隔离、参数边界与缺失日标注。

## Setup

- 租户 T 下已纳管账号集合中至少一个账号存在磁盘且 `ecam_disk_metric` 已有若干日指标行
- 存在至少一行 `qc_status=zero_exception`(usage_percent=0 异常)记录用于验证读取闭环
- 用户已持有租户 T 的有效鉴权凭证

## Happy Path

### Step 1: 查询单盘趋势

**User Action**: 用户调用 `GET /assets/disk/metrics?disk_id=<盘ID>&account_id=<账号>&days=30`。

**Expected Result**: 200 返回该盘 30 天内逐日趋势,同时包含「最新一天」值与「近 N 天均值」两类值;缺失日以 data_status 标注,不填假值。

### Step 2: 查询账号视角 Top 榜

**User Action**: 用户调用 `GET /assets/disk/top?account_id=<账号>&days=7&sort=usage_percent&top=10&page=1&page_size=20`。

**Expected Result**: 200 返回按近 N 天均值口径排序的磁盘 Top 列表,含分页元数据;`sort` ∈ usage_percent|iops|throughput;`top` 默认 10 最大 50。

### Step 3: 分辨 qc_status 异常行

**User Action**: 用户查看趋势/Top 响应中 usage_percent=0 的磁盘。

**Expected Result**: 写路径的 `qc_status=zero_exception` 在读取响应中原样暴露并映射进 `data_status`,前端可分辨「使用率 0 是异常」而非当正常空盘。

## Edge Cases

### Step 1b: 未认证请求

**Precondition**: 请求未携带有效鉴权凭证(token 缺失或过期)。

**User Action**: 无凭证调用任一 Disk 指标读取接口。

**Expected Result**: 401 Unauthorized,响应体含认证错误信息,不泄露敏感数据。

### Step 2a: 越权查询租户外账号

**Precondition**: 客户端传入的 `account_id` 不属于当前租户的账号集合。

**User Action**: 调用趋势或 Top 接口指定该 account_id。

**Expected Result**: 返回 404(不泄露账号存在性);响应中无该账号任何数据。

### Step 2b: 参数非法

**Precondition**: 请求参数越界(如 `days=0`、`days=91`、`sort=latency`、`top=100`)。

**User Action**: 调用趋势或 Top 接口携带非法参数。

**Expected Result**: 400 Bad Request,响应体明确列出校验失败项(days 限 1~90,sort 枚举校验,top 上限 50)。

### Step 3a: 请求区间完全无数据

**Precondition**: 指定盘在请求 days 区间内无任何指标行(如启用日前或新纳管盘)。

**User Action**: 调用单盘趋势查询。

**Expected Result**: 返回空趋势列表或全 data_status 标注的空态语义(区分「无数据」与「采集失败/未启用」),不构造假值;近 N 天均值为空/标注而非 0。

## Journey Invariants

- 租户隔离恒成立:所有读取接口从鉴权上下文取 tenantID,account_id 校验失败一律 404 且不泄露账号存在性
- 参数边界恒成立:days ∈ [1,90],sort ∈ {usage_percent, iops, throughput},top 默认 10 最大 50
- 诚实数据:缺失日以 data_status 标注,任何路径不填假值、不把异常 0 当正常值
- 只读无副作用:读取接口不产生状态变更,重复调用幂等

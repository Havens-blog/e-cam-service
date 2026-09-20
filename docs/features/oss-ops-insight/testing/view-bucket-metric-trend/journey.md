---
feature: "oss-ops-insight"
journey: "view-bucket-metric-trend"
risk_level: "Low"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/oss-ops-insight/proposal.md
generated: "2026-09-20"
---

# Journey: view-bucket-metric-trend

**Risk Level**: Low

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

用户查看单个 OSS bucket 的容量/对象数趋势：单 bucket 趋势读取（最新一天 + 近 N 天均值，`days` 窗口 1~90）、缺失日 data_status 标注不填假值、`qc_status=zero_exception` 读取侧闭环、空态区分「无数据」与「采集失败/未启用」（Key Scenario 5、SC-5）。

## Setup

- 目标 bucket 在 `ecam_oss_metric` 中已有 ≥1 天指标行；租户下账号已纳管
- 用户已通过鉴权（合法 tenantID 与凭证）
- 部分日期缺失（模拟采集断档）用于验证 data_status

## Happy Path

### Step 1: 查看单 bucket 容量/对象数趋势

**User Action**: 用户打开 bucket 抽屉「监控」tab，请求该 bucket 的天粒度指标序列。

**Expected Result**: 返回容量（GB）与对象数两条序列，同时含「最新一天」与「近 N 天均值」两类值；前端渲染容量柱 + 对象数线双轴趋势图。

### Step 2: 指定天数窗口查看

**User Action**: 用户以 `days=30` 请求趋势窗口。

**Expected Result**: 返回近 30 天窗口内的指标序列；`days` 在 1~90 范围内均被接受。

### Step 3: 缺失日以 data_status 标注

**User Action**: 用户查看窗口内存在采集断档的日期点。

**Expected Result**: 缺失日通过 data_status 显式标注（缺失即缺失），不填假值（不用 0 或邻近值冒充）。

### Step 4: zero_exception 行原样暴露

**User Action**: 用户查看 `qc_status=zero_exception`（storage_size=0 异常）行在趋势中的呈现。

**Expected Result**: 读取响应原样暴露 qc_status 并映射进 data_status；前端可分辨「容量为 0 是异常」而非当正常空桶。

## Edge Cases

### Step 1b: bucket 无任何指标数据

**Precondition**: 新纳管的 bucket 尚无任何采集行。

**User Action**: 请求该 bucket 趋势。

**Expected Result**: 空态返回（非报错），前端展示「无数据」空态；不构造空趋势假图。

### Step 1c: 采集失败/未启用的空态区分

**Precondition**: bucket 所属厂商采集持续失败或指标采集未启用。

**User Action**: 请求该 bucket 趋势并观察空态。

**Expected Result**: 空态能区分「采集失败/未启用」与「无数据」，采集异常时展示警示而非纯空。

### Step 1d: 未授权访问

**Precondition**: 请求未携带有效凭证（或凭证过期）。

**User Action**: 无凭证请求单 bucket 趋势接口。

**Expected Result**: 返回 401 Unauthorized，响应体含认证错误信息，不泄露敏感数据。

### Step 2b: days 参数越界

**Precondition**: 客户端传入 `days=0` 或 `days=91`。

**User Action**: 请求趋势接口。

**Expected Result**: 参数校验拒绝（400 类响应），明确提示 days 限 1~90；不越界查询。

### Step 4b: 容量为 0 时使用率派生不 panic

**Precondition**: 某行 storage_size=0，读取侧需要派生使用率类数值。

**User Action**: 请求包含该行的趋势/统计响应。

**Expected Result**: used/capacity 边界处理：容量为 0 时使用率派生为 null 而非除零 panic/NaN，接口正常返回。

## Journey Invariants

- 读取侧一律以 `ecam_oss_metric` 指标表为唯一数据来源，资产表快照数值不出现在趋势响应中
- 缺失日只做 data_status 标注，绝不填假值（0/邻近值冒充）
- qc_status 写入态在读取响应中原样可辨，异常零与正常空桶可区分
- `days` 窗口语义固定为 1~90 天，越界一律校验拒绝

---
feature: "nas-ops-insight"
journey: "view-fs-metric-trend"
risk_level: "Low"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/nas-ops-insight/proposal.md
generated: "2026-09-19"
---

# Journey: view-fs-metric-trend

**Risk Level**: Low

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

运营查看某 NAS 文件系统近 30 天容量/使用率趋势（抽屉「监控」tab 数据来源，Key Scenario 1），据此判断是否需要扩容。纯读取 Journey，覆盖单实例趋势接口契约：days[] 升序、缺失日 data_status 标注、utilization 读取侧派生、零容量异常行可分辨。

## Setup

- 目标 fs_id 在 `ecam_nas_metric` 中已有若干日行（含至少一行正常非零行）
- 运营已登录且持有目标租户资产查看权限

## Happy Path

### Step 1: 请求单实例趋势

**User Action**: 运营打开 NAS 实例抽屉「监控」tab，请求 `GET /assets/nas/metrics?fs_id=<fs>&account_id=<acc>&days=30`。

**Expected Result**: 返回 200 与 `{ fs_id, days[] }`；days[] 按日期升序；每项含 `date / capacity / used / utilization / data_status / qc_status`。

### Step 2: 解读派生使用率与数据状态

**User Action**: 运营查看趋势中 utilization 曲线与 data_status/qc_status 标注。

**Expected Result**: utilization 由 capacity/used 读取时派生（非独立落库字段）；`used>capacity` 时按 `min(used, capacity)` 收敛参与计算且原始 used 仍返回；`used=0, capacity>0` 时 utilization=0；capacity=0 行 utilization 为 null 且 `data_status=zero_exception` 可分辨。

### Step 3: 判断扩容时机

**User Action**: 运营基于近 30 天水位（日值序列最大值即近 N 天峰值口径）判断扩容时机。

**Expected Result**: 趋势数据足以表达「近 N 天里水位最高的那一天」；不承诺日内尖峰（日末态快照架构一致）。

## Edge Cases

### Step 1b: 回看天数越界

**Precondition**: 请求 days=0 或 days=91。

**User Action**: 请求趋势接口。

**Expected Result**: 400 参数校验失败，响应体列出校验失败项；days 合法域为 1~90。

### Step 1c: 请求不存在的 fs

**Precondition**: fs_id 不存在或 account_id 不属于当前租户。

**User Action**: 请求趋势接口。

**Expected Result**: 404，不泄露资源/账号存在性；未携带有效鉴权时 401。

### Step 2b: 缺失日不填充假值

**Precondition**: 回看窗口内某几日无指标行（采集未启用日/厂商缺口）。

**User Action**: 请求趋势接口。

**Expected Result**: 缺失日以 `data_status` 标注，不填充 0 或假值；有数据日返回真实值。

### Step 2c: 零容量异常行

**Precondition**: 某日行 `qc_status=zero_exception`（capacity=0）。

**User Action**: 请求趋势接口。

**Expected Result**: 该行原样暴露 qc_status 并映射 `data_status=zero_exception`，utilization=null；前端渲染警示/异常标记而非当作正常零容量。

### Step 2d: 多账号同 fs 各看各的

**Precondition**: 同一 fs_id 被 ≥2 账号采集（多活/共享实例）。

**User Action**: 分别以两个 account_id 请求同一 fs 趋势。

**Expected Result**: 趋势接口按账号保留各自行，各自返回各自账号视角的日值序列，互不混合。

## Journey Invariants

- 趋势响应中 utilization 永远由 capacity/used 现场派生，响应不出现 NaN，capacity=0 时 utilization 为 null
- days[] 永远按日期升序返回；缺失日只以 data_status 标注，绝不用 0/假值填充
- 越权 account_id 一律 404 且不泄露账号存在性；未认证一律 401
- 界面趋势数据仅来自 `ecam_nas_metric` 指标表，不混用资产表 capacity/used_capacity

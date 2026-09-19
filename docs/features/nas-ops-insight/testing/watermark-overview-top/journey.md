---
feature: "nas-ops-insight"
journey: "watermark-overview-top"
risk_level: "Low"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/nas-ops-insight/proposal.md
generated: "2026-09-19"
---

# Journey: watermark-overview-top

**Risk Level**: Low

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

运营在 NAS 列表页看整体存储水位：顶部运营卡（总容量/已用容量/平均使用率）与账号视角 Top 排行（Key Scenario 2）。核心是聚合口径——按 fs_id 去重防共享 fs 双计、平均使用率分母跳过无数据与零容量实例、采集失败警示优先于 0 占位。

## Setup

- 租户下存在多个账号与多个 NAS 实例，其中至少一对「多账号同 fs_id 并存」的共享实例
- 至少一个实例有正常非零指标行，一个实例指标行 capacity=0（zero_exception）
- 运营已登录且持有租户资产查看权限

## Happy Path

### Step 1: 查看运营卡水位聚合

**User Action**: 运营打开 NAS 列表页，查看顶部运营卡的总容量/已用容量/平均使用率。

**Expected Result**: 聚合按 `fs_id` 去重后计数：同一物理文件系统只计一次；多账号并存时按「日期 desc 再容量 desc」取第一行，该行 capacity/used 作为该物理 fs 的容量口径；不做跨账号容量求和或平均。

### Step 2: 查看 Top 排行

**User Action**: 运营请求 `GET /assets/nas/top?account_id=&days=&sort=utilization&top=10&page=1&page_size=10`。

**Expected Result**: 返回 `{ total, page, page_size, items[] }`；items 按 sort 字段（近 N 天均值口径，utilization 为近 N 天均值）降序取前 N（默认 10，最大 50）；每条含 fs_id / fs_name / account_id 列表 / data_status / qc_status / 最新一天与近 N 天均值两类 capacity·used·utilization。

### Step 3: 识别高水位实例

**User Action**: 运营依据 Top 排行与运营卡定位高水位实例。

**Expected Result**: 排行反映真实水位；共享 fs 不因多账号并存而双计失真；高水位实例可下钻到抽屉趋势。

## Edge Cases

### Step 1b: 多账号共享 fs 去重

**Precondition**: 同一 fs_id 有 ≥3 账号各留一行指标。

**User Action**: 查看运营卡与 Top。

**Expected Result**: 该物理 fs 只计一次（取「日期 desc 再容量 desc」第一行），总容量/Top 不因共享被双计。

### Step 1c: 平均使用率分母收敛

**Precondition**: 实例集合中存在无数据实例与 capacity=0 异常实例。

**User Action**: 查看运营卡平均使用率。

**Expected Result**: 无数据实例跳过不参与分母（不记 0 拉低均值）；capacity=0 行也不参与均值、作为异常单独可见。

### Step 1d: 全部采集失败显示警示

**Precondition**: 某账号/厂商全部实例当日采集失败（执行器任务 Result 失败计数 > 0）。

**User Action**: 查看运营卡。

**Expected Result**: 「采集失败→警示」优先于「无数据→0 占位」——显示警示而非 0 占位；仅任务成功执行且指标真实为 0/空才落入 0 占位分支。

### Step 2b: Top 参数越界与非法排序

**Precondition**: 请求 top=51 或 page_size=51（超最大 50），或 sort 传非法值。

**User Action**: 请求 Top 接口。

**Expected Result**: 400 参数校验失败并列出失败项；sort 合法域为 `capacity|utilization`。

### Step 2c: 越权 account_id

**Precondition**: 客户端传入非本租户的 account_id。

**User Action**: 请求 Top 接口。

**Expected Result**: 404 不泄露账号存在性；未认证 401。

### Step 2d: 空集合分页边界

**Precondition**: 账号下无任何 NAS 指标行，或 page 超出总页数。

**User Action**: 请求 Top 接口。

**Expected Result**: 返回 200 与空 items[] 及正确 total/page/page_size 元数据，不报错。

## Journey Invariants

- 任何聚合视图（运营卡/Top）必须先按 fs_id 去重再计数，共享 fs 永不双计
- 聚合口径以最新日期行的厂商返回值为准，绝不跨账号容量求和或平均
- 「警示」判定永远优先于「0 占位」；未知（采集失败）不得伪装成真零容量
- 平均使用率的分母永不包含无数据实例与 capacity=0 异常实例
- Top 的 sort 仅接受 capacity|utilization，utilization 一律取近 N 天均值口径

---
feature: "log-query-optimization"
journey: "detail-grouping-pagination"
risk_level: "Low"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/log-query-optimization/proposal.md#key-scenarios
  - docs/proposals/log-query-optimization/proposal.md#proposed-solution
  - docs/proposals/log-query-optimization/proposal.md#success-criteria
generated: "2026-09-16"
---

# Journey: detail-grouping-pagination

**Risk Level**: Low

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

用户在多云大结果集（如 84 个 WAF 源混排）下阅读明细：明细按「云 · 账号」折叠分组展示，组头显示条数/耗时，用户按需展开某账号查看其域名与日志，并在组内翻页，不重查、不丢状态。

对应 Proposal「Key Scenario 4」、Proposed Solution 第 4 条 b)（按云·账号折叠分组）与 Success Criteria 第 4 条。

## Setup

- 已完成一次跨多云的成功查询，结果包含多个云/账号的明细（典型：71 Akamai 源 + 13 国内源）。
- 明细分屏滚动、表头吸顶布局生效。

## Happy Path

### Step 1: 查看按云·账号的折叠分组

**User Action**: 用户浏览结果区的明细部分。

**Expected Result**: 明细按云·账号折叠分组组织（不再全部混排），每个组头显示该组条数与耗时；不展开也能掌握各组的量级。

### Step 2: 展开某个分组

**User Action**: 用户点击展开某账号的折叠组。

**Expected Result**: 该组的明细行展示出来（含该账号的域名维度信息），其余组保持折叠；布局自适应，表头吸顶滚动不乱。

### Step 3: 收起已展开的分组

**User Action**: 用户再次点击该组头收起。

**Expected Result**: 该组明细行隐藏，组头的条数/耗时信息仍然完整可见。

### Step 4: 组内翻页

**User Action**: 用户在某个展开的组内翻到下一页。

**Expected Result**: 该组加载并显示下一页明细，组头条数统计保持准确（显示组内总数而非已加载页行数）；翻页不触发整页重查，其他组状态不受影响。

### Step 5: 展开多个分组对照阅读

**User Action**: 用户同时展开多个云的分组。

**Expected Result**: 各组独立展示与独立翻页，互不干扰；明细分屏滚动仍流畅，表头保持吸顶。

## Edge Cases

<!-- Low risk: happy path + critical error paths only. -->

### Step 1b: 仅一个源的查询结果

**Precondition**: 筛选后结果只来自单个云账号。

**User Action**: 查看明细分组。

**Expected Result**: 分组结构仍然成立（单组呈现），组头信息完整，不因单组而退化成错误布局。

### Step 1c: 某分组零条目

**Precondition**: 某账号本次查询命中 0 条日志。

**User Action**: 查看该组。

**Expected Result**: 组头显示 0 条（含耗时），组内无明细行；展开为空态或禁用，不报错。

### Step 4b: 翻到末页后再翻

**Precondition**: 某组已翻至最后一页。

**User Action**: 再次点击下一页。

**Expected Result**: 下一页入口禁用或无操作，不报错、不重复渲染末页数据。

### Step 4c: 翻页时改变页大小

**Precondition**: 用户在组内修改每页条数。

**User Action**: 应用新页大小。

**Expected Result**: 该组回到第一页并以新页大小加载，组头总数统计保持一致；其他组不受影响。

### Step 4d: 翻页后收起再展开

**Precondition**: 某组已翻到第 2 页或更后。

**User Action**: 收起该组再重新展开。

**Expected Result**: 组状态一致（页码或回到首页的行为二者取其一且全局一致），组头条数始终准确，不产生重复行。

## Journey Invariants

- 组头的条数统计始终反映该组总数，而非当前已加载页的行数。
- 折叠/展开操作永不触发重新查询，也永不丢失其他组的展开与页码状态。
- 各组的翻页状态相互独立，一组翻页不影响另一组。
- 组头信息（条数/耗时）无需展开即可读，且为文本化呈现。

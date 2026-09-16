---
feature: "log-query-optimization"
journey: "aggregate-drilldown-clear"
risk_level: "Medium"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/log-query-optimization/proposal.md#key-scenarios
  - docs/proposals/log-query-optimization/proposal.md#proposed-solution
  - docs/proposals/log-query-optimization/proposal.md#success-criteria
generated: "2026-09-16"
---

# Journey: aggregate-drilldown-clear

**Risk Level**: Medium

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

用户通过 TopN 分组统计图定位异常项（如某状态码或某攻击源占比异常），点击该条形项后系统自动为该分组值加字段筛选并重查——把"看到异常 → 钻到具体日志"合并为一次点击；随后一键清除筛选回到原始视图。

对应 Proposal「Key Scenario 3」、Proposed Solution 第 4 条 c)（TopN 点击下钻）与 Success Criteria 第 6 条。

## Setup

- 已完成一次成功查询，统计区展示三图 + 趋势（含 TopN 分组条形图），图与明细共用同一查询样本。
- TopN 图至少有一个分组项有非零计数。

## Happy Path

### Step 1: 查看 TopN 分组图

**User Action**: 用户浏览统计区的 TopN 分组图。

**Expected Result**: 图表展示各分组值的计数排行（TopN），栅格/高度随分组数自适应，不溢出不挤压。

### Step 2: 点击某 TopN 条形项

**User Action**: 用户点击图中某个分组项（如 404 那一条）。

**Expected Result**: 系统自动为该分组值添加字段筛选并触发重查——无需用户手动打开筛选控件。

### Step 3: 查看下钻结果

**User Action**: 用户查看重查后的明细。

**Expected Result**: 明细仅包含匹配该分组值的日志；该筛选在筛选区可见可编辑（与手工筛选同构）。

### Step 4: 从下钻继续切换到另一分组

**User Action**: 用户在仍处于下钻态时点击另一个 TopN 条形项。

**Expected Result**: 原分组值筛选被替换为新的分组值（非叠加、非重复），重查结果只反映新分组值。

### Step 5: 一键清除下钻筛选

**User Action**: 用户点击清除筛选回到原位。

**Expected Result**: 下钻产生的字段筛选被移除，重查恢复到下钻前的完整视图，日志类型与时间范围不变。

## Edge Cases

### Step 2b: 对零计数/极小分组下钻

**Precondition**: 用户点击的分组项计数为 0 或极小，下钻查询结果为空。

**User Action**: 点击该条形项。

**Expected Result**: 正常加筛选重查并展示空结果态（含已加筛选的可视反馈），不报错。

### Step 2c: 连续快速双击同一项

**Precondition**: 用户误触双击同一个 TopN 项。

**User Action**: 快速点击两次同一分组项。

**Expected Result**: 只产生一次筛选与一次重查，不出现重复条件或两份结果。

### Step 2d: 聚合查询慢/冷启动

**Precondition**: `POST /logs/aggregate` 处于冷启动或扫描量大。

**User Action**: 点击 TopN 项触发重查。

**Expected Result**: 重查期间展示与查询一致的进行中状态（已耗时），完成后更新图表与明细；失败时按可读错误 + 重试处理。

### Step 3b: 下钻态下叠加手工筛选

**Precondition**: 已通过图表下钻加入一个字段筛选。

**User Action**: 用户再通过筛选控件手工添加其他字段条件。

**Expected Result**: 图表产生的筛选与手工筛选按组合语义共存，重查结果同时满足两者；两者均可分别清除。

### Step 5b: 无下钻时执行清除

**Precondition**: 用户未点击过任何 TopN 项，无图表产生的筛选。

**User Action**: 点击清除筛选。

**Expected Result**: 空操作无副作用——不重查、不改变当前视图。

### Step 5c: 下钻态修改时间范围

**Precondition**: 处于某分组下钻态。

**User Action**: 用户更改时间范围并重查。

**Expected Result**: 分组筛选与新的时间范围组合生效；图表随新样本刷新，下钻筛选保持有效（除非用户主动清除）。

## Journey Invariants

- 图表下钻与手工字段筛选复用同一套统一筛选机制——下钻产生的条件在筛选区始终可见、可编辑、可清除。
- 点击 TopN 项产生的筛选是"替换该字段取值"语义，永不叠加重复条件。
- 清除下钻后必须精确恢复下钻前的视图（同样的日志类型、时间范围、其余筛选）。
- 下钻全程不改变日志类型与时间范围。

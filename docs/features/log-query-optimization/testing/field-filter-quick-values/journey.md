---
feature: "log-query-optimization"
journey: "field-filter-quick-values"
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

# Journey: field-filter-quick-values

**Risk Level**: Medium

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

用户在已有查询样本上为字段筛选取值：无需手输，直接从当前样本回填的「快捷值」（状态码集合/热门域名/动作枚举）中点选，一键重查得到收窄后的结果；快捷值零额外请求，贴合"看 404、找攻击源"的运维高频路径。

对应 Proposal「Key Scenario 3」、Proposed Solution 第 4 条 a)（字段筛选取快捷值）与 Success Criteria 第 5 条。

## Setup

- 已完成一次成功的日志查询（见 `log-query-lifecycle`），结果区有可用样本数据。
- 结果样本中包含状态码、域名、动作等字段值（如 200/404/503、热门攻击源 IP/域名、allow/block/deny）。

## Happy Path

### Step 1: 打开字段筛选

**User Action**: 用户在结果区打开某字段（如状态码）的筛选控件。

**Expected Result**: 筛选控件展示该字段的「快捷值」候选——来自当前已返回样本的常见值（如 200、404、503），无需用户记忆或手输。

### Step 2: 点选快捷值

**User Action**: 用户点击某个快捷值（如 404）。

**Expected Result**: 该值被回填到筛选输入中（降维为点选），用户可继续叠加或直接确认。

### Step 3: 应用筛选并重查

**User Action**: 用户确认筛选条件触发重查。

**Expected Result**: 系统以新筛选条件重新查询，结果收窄为匹配该值的日志（如全部 404 记录），耗时表现与常规查询一致（热 <300ms / 冷 <3s 目标）。

### Step 4: 对其他字段重复快捷取值

**User Action**: 用户对域名、动作字段分别使用快捷值筛选（如选热门攻击域名、选 deny 动作）。

**Expected Result**: 各字段快捷值均来自样本回填（热门域名/动作枚举），多条件叠加后查询语义正确（条件之间为既定组合语义），结果相应收窄。

### Step 5: 清除字段筛选

**User Action**: 用户清除已应用的字段筛选。

**Expected Result**: 筛选被移除，结果恢复到未筛选的完整查询结果，日志类型与时间范围保持不变。

## Edge Cases

### Step 1b: 样本为空

**Precondition**: 当前查询结果为空或样本中该字段无值。

**User Action**: 打开该字段的筛选控件。

**Expected Result**: 快捷值区域为空或隐藏（不显示误导性候选），手动输入仍可用。

### Step 1c: 高基数字段

**Precondition**: 某字段（如客户端 IP）取值非常多，样本仅覆盖其中一小部分。

**User Action**: 打开该字段筛选。

**Expected Result**: 快捷值仅展示样本中的常见值（有限集合），不假装穷举全集；用户仍可手输任意值。

### Step 3b: 筛选后零结果

**Precondition**: 所选快捷值组合在时间范围内没有匹配日志。

**User Action**: 应用筛选重查。

**Expected Result**: 展示空结果态，已选筛选值保留在筛选区供用户修改或清除，不报错。

### Step 3c: 手输与快捷值混用

**Precondition**: 用户已手输一个筛选值，又点选了一个快捷值。

**User Action**: 同时应用手输值与点选值。

**Expected Result**: 两个条件按组合语义正确生效，不互相覆盖、不产生非法重复条件。

### Step 4b: 多条件叠加后逐个清除

**Precondition**: 已叠加多个字段的筛选。

**User Action**: 逐个移除部分筛选条件。

**Expected Result**: 每次移除后重查结果与剩余条件语义一致；最后清空时恢复完整结果。

### Step 5b: 清除时本无筛选

**Precondition**: 当前没有应用任何字段筛选。

**User Action**: 点击清除筛选。

**Expected Result**: 无副作用空操作——不发起重查、不改变当前结果与查询条件。

## Journey Invariants

- 快捷值只来源于已返回的样本数据，取值过程不产生任何额外网络请求。
- 快捷值是候选建议，不限制用户手动输入任意合法值。
- 字段筛选的任何变更不改变已选日志类型与时间范围。
- 筛选状态在重查、空结果、失败等场景下均保持可见可编辑。

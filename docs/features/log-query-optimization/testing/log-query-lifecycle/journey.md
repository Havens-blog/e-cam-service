---
feature: "log-query-optimization"
journey: "log-query-lifecycle"
risk_level: "Medium"
golden_path: true
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/log-query-optimization/proposal.md#key-scenarios
  - docs/proposals/log-query-optimization/proposal.md#proposed-solution
  - docs/proposals/log-query-optimization/proposal.md#success-criteria
  - docs/proposals/log-query-optimization/proposal.md#non-functional-requirements
generated: "2026-09-16"
---

# Journey: log-query-lifecycle

**Risk Level**: Medium

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

**Golden Path**: 本 Journey 为该 feature 的 Golden Path（Complex 级：日志类型 → 云账号/源 → 日志条目 三层实体关联），覆盖主用户故事「发起查询 → 观察进度 → 读取结果 → 从失败恢复」的完整核心域动作序列。

## Overview

用户选定日志类型与时间范围后发起日志查询，查询期间看到明确的进行中状态（正在查询 N 个云账号、已耗时），完成后按源读取结果；遇到慢查询、单源失败、整体失败时通过可读的错误信息与重试恢复，而不是死屏或裸错误串。

对应 Proposal「Key Scenario 2」、Proposed Solution 第 2 条（加载等待态 + 失败重试）与 Success Criteria 第 3 条。

## Setup

- 已进入日志查询页，且已选定 WAF 日志类型、源清单可用（见 `waf-source-browsing`）。
- 多云账号已接入，查询涉及多个云账号（如 Akamai + 国内云 + AWS）。
- 字段字典（`/types`）可用，动态列由字典驱动。

## Happy Path

### Step 1: 设定查询条件

**User Action**: 用户确认日志类型（WAF）并选择时间范围。

**Expected Result**: 查询条件就绪，字段筛选区由字段字典驱动渲染动态列，无报错。

### Step 2: 发起查询

**User Action**: 用户点击查询按钮。

**Expected Result**: 页面立即进入明确的进行中状态：显示"正在查询 N 个云账号…"步骤态与已耗时计时，而非仅骨架 shimmer 死屏。

### Step 3: 等待查询完成

**User Action**: 用户等待查询结束（期间观察进度态）。

**Expected Result**: 进度态持续展示已耗时；`POST /logs/search` 热缓存 <300ms、冷启动 <3s（AWS 源冷启动单独放宽并标注）；完成后进度态消失并展示结果与总耗时。

### Step 4: 按源读取结果

**User Action**: 用户浏览查询结果明细。

**Expected Result**: 结果按源组织展示，每个源展示自己的条数与状态；某源失败仅标注该源并给出可读原因，其余源结果正常呈现（部分失败不整页失败）。

### Step 5: 重复执行同一查询

**User Action**: 用户条件不变再次点击查询。

**Expected Result**: 第二次查询命中进程级缓存明显更快，结果与首次一致；不产生重复/叠加的结果区块。

## Edge Cases

### Step 2b: 查询执行期间重复点击查询

**Precondition**: 上一轮查询仍在进行中（进度态可见）。

**User Action**: 用户再次点击查询按钮。

**Expected Result**: 不发起叠加的重复查询（进行中的查询被复用或替换），结果区不会渲染两份结果；页面状态保持一致。

### Step 2c: 全部源查询失败

**Precondition**: 所有云账号同时不可用（网络中断或凭证失效）。

**User Action**: 点击查询。

**Expected Result**: 整页进入失败态，展示具体可读原因与"重试"按钮；错误为业务可读信息，不是裸系统错误串；点击重试重新发起同一查询。

### Step 3b: 查询超时或结果截断

**Precondition**: 时间范围过大或扫描量大，导致查询超时/被截断。

**User Action**: 发起查询并等待。

**Expected Result**: 页面给出明确的降级说明（如"结果已截断"或超时提示与重试入口），而非无反馈白屏或裸错误串。

### Step 3c: 单个云源扫描特别慢

**Precondition**: AWS S3 前缀列举/扫码物理性慢。

**User Action**: 发起查询并等待。

**Expected Result**: 进行中状态持续显示已耗时并指明仍在等待的云账号，用户不会误以为系统无响应；不因单源慢而阻塞其他已完成源的展示（按源渐进呈现或明确标注等待中）。

### Step 4b: 部分源失败

**Precondition**: 多源查询中恰好某一个源返回错误。

**User Action**: 查看结果区。

**Expected Result**: 仅该源标记失败并显示可读原因与该源粒度的重试入口，其余源结果完整可用；整页不判失败。

### Step 4c: 查询结果为空

**Precondition**: 所选时间范围与条件下没有任何日志。

**User Action**: 发起查询。

**Expected Result**: 展示明确的空结果态，不报错、不白屏；查询条件保留供用户修改。

### Step 5b: 缓存过期后重查

**Precondition**: 距上次查询超过进程缓存 TTL（10min）。

**User Action**: 再次执行相同查询。

**Expected Result**: 走冷启动路径但仍受冷启动目标约束，结果正确；不出现新旧数据混排或重复结果块。

## Journey Invariants

- 查询期间必须存在文本化的进行中状态（云账号数/已耗时），且可被读屏感知（aria-label），不得仅靠颜色或骨架表达。
- 任何失败必须呈现具体可读的原因与重试入口，禁止裸系统错误串直接透出。
- 单个源失败永不导致整页查询失败；失败粒度不超过该源。
- 查询条件（日志类型/时间范围/字段筛选）在进度态、失败态、空态下均被保留，供用户直接修改或重试。
- 重复发起查询不得产生叠加的重复结果渲染。

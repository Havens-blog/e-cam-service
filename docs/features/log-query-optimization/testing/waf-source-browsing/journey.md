---
feature: "log-query-optimization"
journey: "waf-source-browsing"
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

# Journey: waf-source-browsing

**Risk Level**: Low

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

用户进入日志查询页并选择 WAF 日志类型，系统在可接受时间内返回多云 WAF 源清单（80+ 源），页面不白屏、不因单个云账号失败而整体不可用。

对应 Proposal「Key Scenario 1」与 Success Criteria 第 1 条（`/logs/sources` 热缓存 <300ms、冷启动 <3s）。

## Setup

- e-cam-service 后端服务运行中，`/api/v1/cam/logs/*` 路由可用。
- 多云 WAF 账号已接入（典型环境：71 个 Akamai 源 + 13 个国内源，共 84 源），云凭证有效。
- 后端进程级缓存（10min TTL）状态不限——热缓存与冷启动分别作为边界场景覆盖。

## Happy Path

### Step 1: 打开日志查询页

**User Action**: 用户从导航进入日志查询页面。

**Expected Result**: 页面正常渲染，日志类型选择器可用，无错误提示、无白屏。

### Step 2: 选择 WAF 日志类型

**User Action**: 用户在类型选择器中选择 WAF 日志类型。

**Expected Result**: 系统加载该类型的源清单；热缓存（10min 内已加载过）下响应 <300ms，冷启动 <3s（AWS 源冷启动允许单独放宽并显式标注）；页面始终有加载反馈，不白屏。

### Step 3: 查看源清单

**User Action**: 用户浏览返回的源清单。

**Expected Result**: 80+ 个源全部列出（如 84 个多云 WAF 源），按云/账号维度可辨识，无重复源条目、无空白页面。

## Edge Cases

<!-- Low risk: happy path + critical error paths only. -->

### Step 2b: 冷启动首次加载

**Precondition**: 后端进程刚重启，进程内源缓存为空（冷启动）。

**User Action**: 选择 WAF 日志类型。

**Expected Result**: 响应慢于热缓存但仍在冷启动目标内（<3s；若被 AWS 前缀列举拖累则单独报告放宽），期间页面显示加载态而非死屏；第二次选择时命中缓存显著变快。

### Step 2c: 单个云账号不可用

**Precondition**: 某一个云账号（如某 Akamai 账号）接口不可达或凭证失效。

**User Action**: 选择 WAF 日志类型。

**Expected Result**: 仅该账号的源缺失或标注失败，其余云的源正常列出；页面给出该账号失败的可读提示，不整体报错。

### Step 2d: 重复切换日志类型

**Precondition**: 用户在 WAF 与其他日志类型之间来回切换。

**User Action**: 再次选择 WAF 日志类型。

**Expected Result**: 命中进程级缓存快速返回，源清单不出现重复条目，前一次加载态被正确清除。

### Step 3b: 某类型无可用源

**Precondition**: 所选日志类型在当前租户下没有任何源。

**User Action**: 选择该日志类型并查看清单。

**Expected Result**: 展示明确的空态提示（如"暂无可用源"），而不是空白区域或报错。

## Journey Invariants

- 源清单在任何路径下都不出现重复的源条目。
- 单个云账号的失败永不导致整个源清单页面不可用。
- 加载/失败状态必须文本化呈现（非仅颜色），等待态可被读屏感知（aria-label）。

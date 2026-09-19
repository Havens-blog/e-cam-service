---
feature: "nas-ops-insight"
journey: "vendor-failure-observability"
risk_level: "Medium"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/nas-ops-insight/proposal.md
generated: "2026-09-19"
---

# Journey: vendor-failure-observability

**Risk Level**: Medium

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

弱厂商（尽力而为项 tencent/volcengine，或探测失败降级的厂商）指标名未知或 API 调用失败时返回空、不阻塞其他厂商；失败与「真实无数据」在任务 Result 与前端空态上必须可分辨（Key Scenario 4、SC-2、失败可观测性章节）。涉及任务 Result 状态写入，无不可逆操作，定级 Medium。

## Setup

- 租户下同时存在必达厂商（aliyun/huawei/aws）与尽力而为厂商（tencent/volcengine）的 NAS 实例与账号
- 尽力而为厂商之一处于「API 调用失败」或「探测不支持」状态
- 执行器任务 Result 可查询（运营可查失败计数与末次错误）

## Happy Path

### Step 1: 弱厂商失败返回空不阻塞

**User Action**: 触发 `nas:collect_metrics` 采集，遍历实例时命中失败的尽力而为厂商。

**Expected Result**: 该适配器仅返回自身空结果，其余厂商与全流程正常继续；单实例失败跳过继续。

### Step 2: 失败计数与末次错误进入任务 Result

**User Action**: 运营查看本次执行器任务 Result。

**Expected Result**: Result 按厂商/账号维护失败计数与末次错误（扩展 CDN `skipped_providers` 雏形为含错误明细的结构），失败可查、与成功厂商区分。

### Step 3: 前端空态区分无数据与采集失败

**User Action**: 运营打开失败厂商实例的抽屉监控 tab 与列表页运营卡。

**Expected Result**: 空态区分「无数据（指标真实为 0 或空）」与「采集失败/未启用」；采集异常时运营卡显示警示而非与真实无数据相同的纯空。

## Edge Cases

### Step 1b: 探测不支持 vs 调用失败分级日志

**Precondition**: 适配器遇到「指标名/namespace 未知（探测不支持）」；另一实例遇「API 错误/超时/鉴权失败」。

**User Action**: 触发采集并检查两类失败路径日志。

**Expected Result**: 探测不支持打 INFO；调用失败打 ERROR 并携带 error 字段——两条失败路径在日志级别与内容上可分辨。

### Step 2b: 必达厂商连续零成功升级告警

**Precondition**: 某必达厂商（存在 ≥1 个 NAS 实例）连续 3 天（默认 N=3）当日零成功写库行。

**User Action**: 执行器每日健康检查运行。

**Expected Result**: 触发升级告警（钉群/日志 ERROR → 页面级），走与日闸写失败共用同一告警通道。

### Step 2c: 无实例厂商不触发零成功告警

**Precondition**: 某必达厂商当前在 `ecam_instance` 枚举中 NAS 实例数为 0。

**User Action**: 执行器健康检查运行。

**Expected Result**: 不触发零成功告警（前置条件不满足），避免稳定误报造成告警疲劳。

### Step 2d: capacity=0 不继承全零跳过过滤

**Precondition**: CDN 执行器有「当日全零即跳过不写库」过滤；NAS 某实例厂商返回 capacity=0。

**User Action**: 采集写入该行。

**Expected Result**: NAS 不继承该过滤，capacity=0 异常行标记 `qc_status=zero_exception` 后落库可见（否则华为/AWS 首日整表静默为空）。

### Step 3b: 失败恢复后警示解除

**Precondition**: 失败厂商次日恢复成功写库。

**User Action**: 运营查看该厂商实例空态与运营卡。

**Expected Result**: 空态恢复为正常数据态/0 占位语义，警示随失败计数清零解除，不残留过期警示。

## Journey Invariants

- 任何单厂商/单实例失败只影响自身，全流程永不因尽力而为厂商失败而阻塞或报错
- 「探测不支持」与「调用失败」永远可分辨（INFO vs ERROR+error 字段）；失败必须出现在任务 Result 中，绝不静默吞掉
- 前端空态语义三分：真实无数据 / 采集失败或未启用 / 零容量异常（zero_exception），三者不得混同展示
- 零成功告警只对「实盘存在 ≥1 个 NAS 实例的必达厂商」生效
- capacity=0 的异常行永远落库可见，不受任何「全零跳过」类过滤影响

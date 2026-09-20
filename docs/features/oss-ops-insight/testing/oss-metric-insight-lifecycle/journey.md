---
feature: "oss-ops-insight"
journey: "oss-metric-insight-lifecycle"
risk_level: "High"
golden_path: true
complexity: "complex"
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/oss-ops-insight/proposal.md
generated: "2026-09-20"
---

# Journey: oss-metric-insight-lifecycle

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

OSS 经营洞察的端到端生命周期（Golden Path）：每日调度认领 oss 日闸 → 按活跃账号遍历 OSS bucket 调厂商监控采集容量/对象数 → 指标行落库 `ecam_oss_metric` → 用户在 OSS 列表页看运营卡、在抽屉「监控」tab 看趋势。覆盖账号→bucket→指标行的跨实体交互链，核心保障是「OSS 界面一律以指标表为唯一数据来源」（Key Scenario 1、SC-3/SC-6）。

**Feature Complexity**: complex（账号 → bucket → 指标行三类实体存在父子/关联关系，Golden Path 须覆盖跨实体交互，5+ 步）。

## Setup

- `scheduler_state` collection 可用，特性开关 `SCHEDULER_PERSISTENT_GATE_ENABLED` 默认开启，日闸已有 `resource_type=oss` 分支
- 至少 1 个活跃账号（已纳管且存在 ≥1 个 OSS bucket）注册在租户下；必达厂商（aliyun/huawei/aws）监控 API 实盘可用
- 调度循环已启动，当前时间在 00:10 之后（采集区间 `[昨日, 今日]`）
- `ecam_oss_metric` collection 已建立唯一键 `(account_id, bucket_name, date)`

## Happy Path

### Step 1: 调度器认领 oss 日闸并触发当日采集

**User Action**: 调度器对 `scheduler_state` 的 oss 键执行原子认领（当日未认领则置为 today），认领成功后提交 `oss:collect_metrics` 采集任务。

**Expected Result**: 当日认领成功，采集任务提交一次；oss 键与 nas/cdn 键按资源类型分键互不干扰。

### Step 2: 按活跃账号遍历 bucket 采集容量/对象数

**User Action**: 执行器按「活跃账号」口径（已纳管且存在 ≥1 个 OSS bucket 的云账号，不依赖 EnableAutoSync 开关）遍历 bucket，厂商适配器调用监控 API 采集当日 storage_size/object_count。

**Expected Result**: 每个 bucket 产出当日容量/对象数指标；容量字节经 `BytesToGB` 归一化（[1MB, 1PB] 数量级门禁）；单厂商/单 bucket 失败只影响自身，不阻塞继续。

### Step 3: 指标行落库（唯一键 upsert 首写生效）

**User Action**: 采集结果按唯一键 `(account_id, bucket_name, date)` upsert 写入 `ecam_oss_metric`（bucket_name/date/storage_size(GB)/object_count/qc_status）。

**Expected Result**: 今日行首写生效（同日重复写不覆盖）；昨日行由次日补采覆盖更新为日末态；同 bucket_name 跨账号各留一行。

### Step 4: 用户在 OSS 列表页查看运营卡

**User Action**: 用户打开 OSS 列表页，查看顶部运营卡的总容量/对象数/近 7 天增速。

**Expected Result**: 运营卡数值全部来自 `ecam_oss_metric` 指标表（资产表 `ecam_instance` 的 storage_size/object_count 快照不显示）；有数据时正常展示，不出现「资产表快照 vs 指标表趋势」同屏矛盾。

### Step 5: 用户在抽屉「监控」tab 查看双轴趋势

**User Action**: 用户点开某个 bucket 的抽屉，切换到「监控」tab，查看容量（柱）+ 对象数（线）双轴趋势图。

**Expected Result**: 趋势图同时呈现「最新一天」与「近 N 天均值」两类值；缺失日由 data_status 标注，不填假值。

### Step 6: 验证采集异常时的警示空态

**User Action**: 在采集失败/未启用的 bucket 上观察运营卡与抽屉趋势的空态表现。

**Expected Result**: 空态区分「无数据」与「采集失败/未启用」；采集异常时运营卡显示警示而非纯空。

## Edge Cases

### Step 1b: 首部署无 oss 日闸记录

**Precondition**: 首部署时 `scheduler_state` 尚无 oss 记录。

**User Action**: 调度器首次触发采集。

**Expected Result**: 视为首次认领，认领后触发一次当日提交（不回溯补采历史），此后按每日一次语义运行。

### Step 1c: 日闸写失败

**Precondition**: 故障注入——认领时写 `scheduler_state` 失败。

**User Action**: 调度器尝试认领 oss 日闸。

**Expected Result**: 指数退避重试并升级告警（SchedulerGateAlerter）；恢复后当日正常认领一次，不静默跳过。

### Step 2b: 必达厂商适配器调用失败

**Precondition**: 某必达厂商（如 aliyun）监控 API 调用失败。

**User Action**: 执行器采集该厂商 bucket 的指标。

**Expected Result**: 该适配器只返回自身空，ERROR 记录 + error 字段 + `Result["failures"]` 计数；不阻塞其他厂商与其他账号继续采集。

### Step 2c: 尽力而为厂商无该指标

**Precondition**: 尽力而为厂商（tencent/volcengine）实盘不支持 OSS 容量指标。

**User Action**: 执行器采集该厂商 bucket 的指标。

**Expected Result**: INFO 日志 + 空返回，不算失败、不进 failures 计数；不阻塞全流程。

### Step 3b: storage_size=0 异常行

**Precondition**: 厂商返回容量为 0（非正常空桶场景）。

**User Action**: 采集结果落库。

**Expected Result**: `qc_status=zero_exception` 落库可见，零值放行不拒绝；读取侧原样暴露并映射进 data_status，前端可分辨「容量为 0 是异常」。

### Step 3c: 容量字节非零越界

**Precondition**: 厂商返回的容量字节换算后非零且超出 [1MB, 1PB] 数量级门禁。

**User Action**: 采集结果尝试落库。

**Expected Result**: 该行被拒绝不写入（非零越界拒绝），失败可观测；不污染 `ecam_oss_metric`。

### Step 6b: 资产表快照与指标表数值不一致

**Precondition**: 资产表 `ecam_instance` 的 storage_size 快照与 `ecam_oss_metric` 最新指标值不一致。

**User Action**: 用户查看 OSS 列表行/运营卡/趋势图的容量数值。

**Expected Result**: OSS 界面一律显示指标表数据，资产表快照数值不出现在 OSS 界面（仅作 bucket 枚举与元数据）。

## Journey Invariants

- OSS 相关界面（列表行容量/对象数、运营卡、趋势图、Top）一律以 `ecam_oss_metric` 指标表为唯一数据来源，资产表快照数值不参与展示
- 采集不依赖账号 EnableAutoSync 开关——活跃账号口径以 `ecam_instance` 枚举存在 ≥1 个 OSS bucket 为准，防非活跃账号下 bucket 静默漏采
- 任一厂商/账号/bucket 的失败只影响自身，绝不阻塞其他厂商/账号/bucket 的采集继续
- 今日行当日内首写生效；昨日行次日补采后冻结；唯一键 `(account_id, bucket_name, date)` 全程有效
- 认领成功是提交采集任务的唯一前提；日闸失败必须走退避重试+告警，绝不静默跳过

---
feature: "disk-ops-insight"
journey: "disk-metrics-daily-collection"
risk_level: "High"
golden_path: true
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/disk-ops-insight/proposal.md
generated: "2026-09-21"
---

# Journey: disk-metrics-daily-collection

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

多云云硬盘指标采集的主链路:每日调度器认领 disk 日闸后,采集执行器按活跃账号遍历云硬盘,逐厂商调用地域性磁盘指标查询(带 region),将使用率/IOPS/吞吐天粒度指标落库 `ecam_disk_metric`;随后用户即可在 Disk 界面(运营卡/抽屉监控 tab)看到当日趋势与近 N 天均值。这是 Golden Path Journey:完整覆盖「日闸认领 → 账号遍历 → 厂商查询 → 指标落库 → 结果汇总 → 用户查看」的跨实体(账号/磁盘/指标/读取接口)核心动作序列。

## Setup

- 租户下已纳管 ≥1 个存在 Disk 实例的云账号(`ecam_instance` 中已有磁盘枚举记录,含 disk_id/region)
- 5 厂商监控 SDK 依赖已在;必达厂商(aliyun/huawei/aws)指标 namespace 已通过探测验证
- `scheduler_state` 日闸机制可用(特性开关 `SCHEDULER_PERSISTENT_GATE_ENABLED` 默认开启)
- `ecam_disk_metric` 集合已建立,唯一键 `(account_id, disk_id, date)` 生效

## Happy Path

<!-- The primary success scenario: steps the user takes to accomplish the goal.
     Each step describes a user action and the expected outcome.
     High-risk Journeys MUST have edge case count >= happy path step count. -->

### Step 1: 调度器认领当日 disk 日闸

**User Action**: 每日调度触发 disk 采集任务,执行器向 `scheduler_state` 的 disk 键发起当日原子认领。

**Expected Result**: 并发认领时仅一个 goroutine 胜出,其余跳过;认领成功后当日采集任务开始执行。

### Step 2: 执行器按活跃账号遍历云硬盘

**User Action**: 采集执行器枚举租户下「已纳管且存在 ≥1 个 Disk 实例」的活跃账号,对每个账号加账号级互斥,账号内对 disk 施加有界并发(复用 `nasAccountGate` 模式)。

**Expected Result**: 活跃账号清单与 `ecam_instance` 枚举一致;不依赖账号 EnableAutoSync 开关;同一账号不会被并发重复采集。

### Step 3: 逐厂商调用磁盘指标查询

**User Action**: 对账号下的每个磁盘,按实例真实 region 调用对应厂商的 DiskMetricQuerier(`GetDiskMetrics(ctx, diskID, diskName, region, startDate, endDate)`),查询当日使用率/IOPS/吞吐。

**Expected Result**: 必达厂商返回归一化指标:usage_percent 统一为 0~100 百分比口径,iops(次/秒)/throughput(MB/s)保留归一单位;查询按地域性资源语义执行,不做全局 region 推断。

### Step 4: 指标落库 ecam_disk_metric

**User Action**: 采集执行器将各磁盘当日指标以补缺式 upsert 写入 `ecam_disk_metric`,唯一键 `(account_id, disk_id, date)`。

**Expected Result**: 每盘每日 ≥1 行落库;同日首写生效(当日已有行不覆盖);usage_percent=0 的 zero_exception 行原样落库可见;容量字节 → GB 走共享 `types.BytesToGB`(如适用)。

### Step 5: 采集结果汇总可观测

**User Action**: 执行器汇总本轮采集结果,失败计数写入 `Result["failures"]`(provider/account/error_count/last_error)。

**Expected Result**: 成功/失败分布可查询;任一适配器失败只计入自身失败计数,不阻塞其他厂商/账号的全流程。

### Step 6: 用户查看单盘趋势

**User Action**: 用户在 Disk 抽屉「监控」tab 打开趋势图(使用率折线 + IOPS/吞吐双轴),或调用 `GET /assets/disk/metrics?disk_id=&account_id=&days=` 查询单盘趋势。

**Expected Result**: 响应包含「最新一天」值与「近 N 天均值」两类值;当日采集落库的行可见;数据一律来自 `ecam_disk_metric` 指标表(资产表快照数值不展示)。

## Edge Cases

<!-- Alternative scenarios where things go wrong or take an unexpected path.
     Each edge case references a happy path step (variant) and describes the
     divergent precondition and expected outcome.
     High-risk Journeys: number of edge cases MUST be >= number of happy path steps. -->

### Step 1b: 首次认领过渡(scheduler_state 无 disk 记录)

**Precondition**: `scheduler_state` 中尚无 disk 键记录(特性启用后的第一天)。

**User Action**: 执行器发起当日认领。

**Expected Result**: 视为首次认领,认领成功后触发一次当日提交;不回溯补采历史(启用日之前的日期无指标行,属预期)。

### Step 2b: 账号无 Disk 实例(非活跃账号)

**Precondition**: 某已纳管账号在 `ecam_instance` 中无任何 Disk 实例。

**User Action**: 执行器遍历活跃账号。

**Expected Result**: 该账号被跳过,不发起厂商查询,不产生指标行,不计入失败。

### Step 3a: 厂商探测不支持该指标

**Precondition**: 某厂商(尽力而为项 tencent/volcengine)监控侧不存在磁盘指标(探测不支持)。

**User Action**: 调用该厂商的 DiskMetricQuerier。

**Expected Result**: 记录 INFO 日志并返回空,不算失败;不写入该厂商行;其余厂商采集不受影响。

### Step 3b: 适配器调用失败

**Precondition**: 某厂商监控 API 调用返回错误(网络/鉴权/限流等)。

**User Action**: 调用该厂商的 DiskMetricQuerier。

**Expected Result**: 记录 ERROR 日志,该账号该厂商的错误计入 error 字段与 `Result["failures"]` 计数;不阻塞其他厂商/账号;仅返回自身空结果。

### Step 4a: 同日重复采集(幂等)

**Precondition**: 当日该 `(account_id, disk_id, date)` 行已存在(调度重跑或手动触发)。

**User Action**: 执行器再次写入当日指标。

**Expected Result**: 今日行保持首写结果不被覆盖;次日补采时昨日行按覆盖更新语义刷新(状态型日快照口径,与 NAS 一致)。

### Step 4b: 磁盘无挂载(available 状态)

**Precondition**: 磁盘处于未挂载状态,使用率语义无意义。

**User Action**: 执行器采集该盘指标。

**Expected Result**: 按探测结论处理:usage_percent 为 null 或 0 并打标(qc_status),读取侧可分辨「未挂载/无意义」而非当正常空盘。

### Step 5a: 必达厂商连续零成功写库

**Precondition**: 某必达厂商(aliyun/huawei/aws)近 3 天零成功写库记录。

**User Action**: 健康检查观测采集结果。

**Expected Result**: 触发健康告警(零成功检测);失败可观测信息(provider/account/error_count/last_error)可用于归因。

### Step 6a: 趋势区间存在缺失日

**Precondition**: 请求趋势的 days 区间内某天无指标行(如启用日之前、厂商当日故障)。

**User Action**: 查询单盘趋势。

**Expected Result**: 缺失日以 data_status 标注(关联写入侧 qc_status 闭环),不填假值;近 N 天均值基于实际存在日计算。

## Journey Invariants

<!-- Cross-step properties that must hold throughout the entire Journey.
     At least one invariant is required per Journey.
     These are verified across all steps, not within a single step. -->

- 唯一键 `(account_id, disk_id, date)` 恒成立:任何时刻同一键至多一行,多账号同 disk_id 并存各留一行,互不覆盖
- 同日首写生效:当日已存在的行永不被同日写入覆盖,昨日行仅由次日补采覆盖更新
- 失败隔离:任一厂商/账号的采集失败不阻塞其他厂商/账号,失败必须可观测(计数/日志/告警),不允许静默吞错
- 数据来源唯一:Disk 界面(趋势/Top/运营卡)一律以 `ecam_disk_metric` 为唯一数据来源,资产表 `ecam_instance` 的 size/iops/throughput 快照数值不得在指标界面展示
- 账号级互斥 + disk 有界并发全程生效,同一账号不会被并发重复采集

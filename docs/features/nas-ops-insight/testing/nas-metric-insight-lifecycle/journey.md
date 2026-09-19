---
feature: "nas-ops-insight"
journey: "nas-metric-insight-lifecycle"
risk_level: "High"
golden_path: true
complexity: "complex"
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/nas-ops-insight/proposal.md
generated: "2026-09-19"
---

# Journey: nas-metric-insight-lifecycle

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

Golden Path（主用户故事完整闭环）：多云 NAS 文件系统的容量/使用率指标经每日采集落入指标库，运营随后通过抽屉趋势与列表页运营卡/Top 消费这些指标，判断扩容时机并识别高水位实例。该 Journey 贯穿「云账号 → NAS 文件系统 → 每日指标行 → 读取接口/聚合视图」的跨实体交互，是本 feature 的核心域动作序列。

**Complexity classification**: Complex（≥2 实体类型且存在父子/关联关系：云账号 → NAS 实例 → 每日指标行；另有 scheduler_state 日闸状态实体）

## Setup

- 租户下已纳管至少 1 个云账号，且该账号在 `ecam_instance` 中存在 ≥1 个 NAS 文件系统实例（含 fs_id、region 元数据）
- NASMetricQuerier 适配器已按厂商注册（必达项 aliyun/huawei/aws 至少一家可用）
- `ecam_nas_metric` 表已建（唯一键 `(account_id, fs_id, date)`）
- 运营已登录且对目标租户有资产查看权限（API 鉴权上下文有效）

## Happy Path

### Step 1: 触发当日 NAS 指标采集

**User Action**: 调度器（00:10 后）触发每日采集；持久化日闸对 `resource_type=nas` 执行 `findOneAndUpdate` 原子认领当日日期，认领成功后按活跃账号（已纳管且存在 ≥1 个 NAS 实例的账号，不依赖 EnableAutoSync 开关）遍历 NAS 实例并提交采集任务。

**Expected Result**: 当日认领成功且仅被一个实例认领；采集任务提交执行；日闸记录更新为今日。

### Step 2: 厂商指标按 GB 口径落库

**User Action**: 各厂商适配器按实例所在 region 调用对应监控 API，将厂商原始字节值在采集边界换算为 GB(二进制 GiB) 后写入 `ecam_nas_metric`，同日行首写生效（仅保护今日行），写入前经数量级自检。

**Expected Result**: 每个「活跃账号 × NAS 实例 × 当日」各落一行 capacity/used_capacity(GB) 记录；utilization 不落库；写入行数量级在 [1MB, 1PB] 区间（capacity=0 行例外放行并打标）。

### Step 3: 运营查看单实例近 30 天趋势

**User Action**: 运营打开某 NAS 实例抽屉「监控」tab，请求该文件系统近 30 天（days=30）的容量/已用/使用率趋势。

**Expected Result**: 趋势接口返回 `{ fs_id, days[] }`，按日期升序，每项含 `date / capacity / used / utilization / data_status / qc_status`；utilization 由 capacity/used 读取侧派生；缺失日以 `data_status` 标注、不填充假值。

### Step 4: 运营查看存储水位运营卡

**User Action**: 运营在 NAS 列表页查看顶部运营卡的总容量/已用容量/平均使用率（近 N 天口径）。

**Expected Result**: 聚合按 `fs_id` 去重后计数（同一物理文件系统只计一次，多账号并存时按「日期 desc 再容量 desc」取第一行作为该物理 fs 的容量口径）；平均使用率对无数据实例与 capacity=0 行跳过不参与分母。

### Step 5: 运营查看 Top 排行识别高水位实例

**User Action**: 运营请求账号视角 Top 排行（sort=utilization, top=10），识别高水位实例作为扩容判断输入。

**Expected Result**: 返回 `{ total, page, page_size, items[] }`，items 按 sort 字段近 N 天均值口径降序取前 N，每条含 fs_id/fs_name/account_id 列表/data_status/qc_status/最新一天与近 N 天均值两类值；共享 fs 不双计。

## Edge Cases

### Step 1b: 服务重启后当日重复触发

**Precondition**: 当日日闸已认领成功后服务进程重启，调度循环重新进入当日窗口。

**User Action**: 重启后调度器再次尝试触发当日 NAS 采集。

**Expected Result**: 日闸条件 `last_date<today` 不满足，认领失败，当日不再重复提交采集任务（修复 CDN 内存闸的重启重复提交缺陷）。

### Step 1c: 多副本同时触发竞争认领

**Precondition**: 多副本部署或手动 `nas:collect_metrics` 与每日自动任务同日重叠。

**User Action**: 多个实例同日同时尝试认领当日。

**Expected Result**: 仅一个实例 `findOneAndUpdate` 认领成功并提交任务，其余认领失败不提交（避免对 CloudWatch 等有配额上限的 API 重复打满）。

### Step 1d: 日闸写失败降级

**Precondition**: mongo 日闸写入失败（网络/主从抖动）。

**User Action**: 采集触发时日闸写操作失败。

**Expected Result**: 走指数退避重试并升级告警（非仅记日志）；不出现「写失败+重启 → 重复提交」回归。

### Step 2b: 某必达厂商适配器调用失败

**Precondition**: aliyun/huawei/aws 中某一家监控 API 宕机或鉴权失效。

**User Action**: 执行器遍历实例采集，命中失败厂商。

**Expected Result**: 该厂商仅返回自身空结果，不阻塞其余厂商与全流程；失败计数与末次错误进入任务 Result 可见。

### Step 2c: capacity=0 异常行落库

**Precondition**: 华为/AWS 实例厂商返回容量为 0（实盘现状）。

**User Action**: 采集写入该日行。

**Expected Result**: 不拦截不跳过（不继承 CDN 全零跳过过滤），标记 `qc_status=zero_exception` 后正常落库；读取侧 `data_status=zero_exception`，utilization 记 null，不 panic、不写 NaN。

### Step 3b: 越权 account_id 查询趋势

**Precondition**: 租户 A 的运营持有租户 A 鉴权上下文，客户端传入租户 B 的 account_id。

**User Action**: 请求 `GET /assets/nas/metrics?fs_id=&account_id=<租户B>&days=30`。

**Expected Result**: 服务端校验 account_id ∉ 租户 A 账号集合，返回 404，不泄露账号存在性；tenant A 查不到 tenant B 的 NAS 指标。

### Step 3c: 回看天数越界

**Precondition**: 客户端传入 days=0 或 days=91（超出 1~90 界）。

**User Action**: 请求趋势接口。

**Expected Result**: 参数校验失败返回 400，响应体列出校验失败项。

### Step 4b: 全部采集失败时运营卡判定

**Precondition**: 某账号/厂商全部实例当日采集失败（任务 Result 失败计数 > 0）。

**User Action**: 运营查看列表页运营卡。

**Expected Result**: 「采集失败→警示」优先于「无数据→0 占位」——显示警示而非纯空或 0 占位；仅任务成功执行且指标真实为 0/空时才落入 0 占位分支。

## Journey Invariants

- 每日每「(account_id, fs_id, date)」至多一行；同日首写生效仅保护今日行，昨日行由次日补采覆盖更新后冻结跨日不可变（Asia/Shanghai 自然日）
- 所有落库容量字段以 GB 为唯一口径，字节到 GB 的换算只发生在采集边界，原始字节永不直写 GB 字段
- utilization 永不落库，任何读取路径均由 capacity/used 现场派生，且 capacity=0 时为 null（无 NaN、无 panic）
- 任何单厂商/单实例采集失败不得阻塞其他厂商/实例的采集与读取（尽力而为语义），且失败必须可观测（失败计数/末次错误/前端空态区分）
- NAS 界面数据一律以 `ecam_nas_metric` 指标表为唯一来源，资产表 capacity/used_capacity 不在 NAS 界面展示容量数值

---
feature: "disk-ops-insight"
journey: "shared-disk-multi-account-metrics"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/disk-ops-insight/proposal.md
generated: "2026-09-21"
---

# Journey: shared-disk-multi-account-metrics

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

跨账号共享盘(同一 disk_id 出现在多个账号)场景下的指标正确性:唯一键 `(account_id, disk_id, date)` 含 account_id 隔离,各账号各留一行;读取侧 Top 按 disk_id 去重取代表行(日期 desc 再使用率 desc),绝不跨账号求和或双计——这是提案中明示的 High 风险缓解路径(「跨账号共享磁盘被误聚合 L/H」)。

## Setup

- 同一租户下 ≥2 个云账号均已枚举到同一 disk_id 的共享盘
- 两账号均已完成当日指标采集,`ecam_disk_metric` 中存在该盘同日的两行(每账号一行)

## Happy Path

### Step 1: 共享盘在多账号下并存落库

**User Action**: 采集执行器分别对账号 A 与账号 B 执行该共享盘的指标采集并落库。

**Expected Result**: `ecam_disk_metric` 中同日存在两行:`(account_A, disk_id, date)` 与 `(account_B, disk_id, date)`,各行独立、互不覆盖。

### Step 2: 账号视角查询看到本账号行

**User Action**: 以账号 A 身份调用 `GET /assets/disk/metrics?disk_id=&account_id=A`。

**Expected Result**: 仅返回账号 A 的行;账号 B 的同行数据不出现;响应含最新一天值与近 N 天均值。

### Step 3: Top 榜按 disk_id 去重取代表行

**User Action**: 调用 `GET /assets/disk/top?account_id=&days=&sort=&top=&page=&page_size=`。

**Expected Result**: 同一 disk_id 在 Top 结果中至多出现一次;代表行取「日期 desc,再使用率 desc」规则选出的行;不跨账号求和,数值不双计。

### Step 4: 运营卡聚合去重后口径

**User Action**: 用户查看 Disk 列表页运营卡(总容量/平均使用率/IO 繁忙盘数)。

**Expected Result**: 聚合统计基于去重后的代表行,共享盘只计一次,不因多账号并存而重复计入容量或盘数。

## Edge Cases

### Step 1b: 两账号各自采集结果不同

**Precondition**: 账号 A 采集成功、账号 B 采集失败(或两账号行值不同)。

**User Action**: 查看该盘两账号的落库结果。

**Expected Result**: 两行各自独立成立或缺失;账号 A 的成功行不被账号 B 的失败影响;账号 B 失败计入 `Result["failures"]` 可观测。

### Step 2b: 查询他账号的共享盘行

**Precondition**: 客户端传入 `account_id=B` 但以账号 A 的鉴权上下文查询,或 B 不属于当前租户账号集合。

**User Action**: 调用单盘趋势接口指定 account_id=B。

**Expected Result**: 服务端校验 account_id ∈ 该租户账号集合,越权返回 404(不泄露账号存在性)。

### Step 3a: 代表行选择规则出现并列

**Precondition**: 两账号行日期相同且使用率相同(排序键并列)。

**User Action**: 调用 Top 榜查询。

**Expected Result**: 按既定规则(日期 desc → 使用率 desc)确定唯一代表行,同一 disk_id 仍只出现一次,结果确定且稳定。

### Step 3b: Top 分页与去重叠加

**Precondition**: 账号下共享盘较多,请求 `top=50`(上限)、分页参数 page/page_size 合法。

**User Action**: 调用 Top 榜分页查询。

**Expected Result**: 去重发生在分页之前;每页内 disk_id 不重复;`top` 默认 10、最大 50,超限请求被拒绝或收敛到上限(按接口契约)。

### Step 4a: 账号被移除后的共享盘行

**Precondition**: 账号 B 从租户移除纳管后,历史行仍留在 `ecam_disk_metric`。

**User Action**: 查询租户视角的 Top 榜与运营卡。

**Expected Result**: 聚合口径基于当前租户可见账号集合;已移除账号的行不进入当前租户聚合,不产生幽灵盘数或虚增容量。

## Journey Invariants

- 唯一键 `(account_id, disk_id, date)` 恒成立:多账号同 disk_id 并存各留一行,任何写入不得跨账号覆盖
- Top 查询对同一 disk_id 至多返回一行(代表行:日期 desc 再使用率 desc),任何读取路径不跨账号求和/双计
- 租户隔离恒成立:account_id 校验失败一律 404 且不泄露账号存在性
- 去重先于分页与聚合:分页结果与运营卡统计均基于去重后的代表行

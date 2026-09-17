---
feature: "cert-volcano-import-sync"
journey: "first-sync-backfill"
risk_level: "High"
golden_path: true
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-volcano-import-sync/proposal.md
generated: "2026-09-17"
---

# Journey: first-sync-backfill

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

首轮（空台账）天级同步把火山等六云证书库实例全量回填入账并建立映射：调度触发同步轮、逐云逐账号枚举、指纹比对识别未入账实例、拉链解析入账、映射建档、会话收敛，后续引用扫描 probe 由 diff 收敛 consistent（3669ddbf 案例自动化）。(Source: proposal.md Key Scenario "Happy path" + "首 run" + Success Criteria 1/5/8)

## Setup

- 台账为空（首 run 语义=全量回填）或不含目标云实例指纹
- 火山账号以 provider volcano/volcengine 登记（active），五云既有账号可用；云侧以 fake/桩承载，不依赖真实火山账号
- 调度点 cert:cert-import 已注册（spec 0 1 * * *），手动触发入口可用

## Happy Path

### Step 1: 天级调度到达触发同步轮

**User Action**: 调度时钟到达 01:00，cert:cert-import 调度点经窄端口驱动同步服务发起本轮多云证书同步

**Expected Result**: 同步服务以 CAS 防重占位成功启动，创建同步会话并标识来源 operator=scheduler，逐云逐账号枚举开始

### Step 2: 枚举全部证书可达云 × active 账号

**User Action**: 同步服务枚举六云（五云+火山）的全部 active 账号

**Expected Result**: 每个云×账号构成独立处理单元；无证书或不可达的云×账号被跳过，不中断其他云

### Step 3: 实例清单与台账指纹比对

**User Action**: 适配器输出实例清单（指纹/SAN/有效期元数据），同步判定层与台账指纹+现有映射比对

**Expected Result**: 空台账下全部 List 实例构成增量集（首 run=全量回填，无需特殊代码）；判定层对已映射指纹不发起任何导入动作

### Step 4: 未入账实例拉链解析入账

**User Action**: 对增量集实例逐个拉取证书链并解析（指纹 sha256/SAN/有效期，口径与 CAS 一致）

**Expected Result**: 台账新增证书记录（沿既有发现导入管线）；revoked/非 issued 实例不入账且会话留痕；单实例失败记 errorReason 不中断后续

### Step 5: 建立云证书映射

**User Action**: 对成功入账实例建立（指纹, 云, 账号）映射

**Expected Result**: 每个实例一条映射且指向新入账证书；重复执行不产生重复映射行

### Step 6: 会话收敛终态并验证 probe 收敛

**User Action**: 等待会话终态，核对逐项结果与台账/映射完整性，并经引用扫描核对 probe 判定

**Expected Result**: 会话收敛 completed；Items[].result 逐项记录导入/跳过/补刷/漂移/失败；台账与映射完整；后续 probe 由 diff 收敛 consistent

## Edge Cases

### Step 1b: 上一轮 running 未结束本轮触发

**Precondition**: 前一轮同步仍在执行（CAS 占位未释放）

**User Action**: 调度点再次触发同步

**Expected Result**: CAS 拒绝，本轮静默跳过不重启不报错（对齐 probe/scan latest-running 模式），在途轮不受影响

### Step 1c: 同步服务未就绪调度触发

**Precondition**: 同步服务依赖未装配（nil）

**User Action**: 调度点到达触发

**Expected Result**: 容忍降级不 panic，调度循环继续正常运行

### Step 2b: 空账号无证书

**Precondition**: 某云某 active 账号下无任何证书实例

**User Action**: 同步枚举该账号

**Expected Result**: 空枚举按跳过处理，不计失败，其余云正常

### Step 3b: 已映射指纹跳过（增量第二轮）

**Precondition**: 台账已含某实例指纹且映射完整

**User Action**: 下一轮同步处理该实例

**Expected Result**: 判定为已同步：不发起导入、不写台账/映射（五云口径 List 即元数据、材料通道 Get 计数为 0；火山口径跳过=不导入，List 内 Get 为适配器固有成本）

### Step 3c: 撤销/审核中实例

**Precondition**: 火山侧实例 IsCertificateRevoked 或 status 非 issued

**User Action**: 同步判定层处理该实例

**Expected Result**: 不入账，会话留痕迹（对齐签发语义）

### Step 4b: 跨云同指纹

**Precondition**: 两云各有一张内容相同（同指纹）的证书进入增量集

**User Action**: 同一轮先后处理两实例

**Expected Result**: 台账恰 1 条，两云各自账号各建映射，两实例均不记失败

### Step 4c: 单实例拉链失败

**Precondition**: 某实例拉链时云 API 失败

**User Action**: 同步继续处理后续实例

**Expected Result**: 失败实例记 errorReason（静态文案），其余收敛，终态按失败语义判定

### Step 6b: 第二轮空转收敛

**Precondition**: 首轮回填完成后无任何云端变化

**User Action**: 再次触发一轮同步

**Expected Result**: 全部实例跳过，零台账/映射写（幂等收敛，只读纪律），会话无失败条目

## Journey Invariants

- 同步执行路径只入账不上线：不调用任何云写方法（只读纪律）
- 台账按指纹全局唯一：任何轮次/并发路径不产生重复台账记录
- 定时轮任意重复执行结果收敛：不产生重复台账/映射
- 单实例/单云失败隔离，不中断其他云与账号
- 云凭证仅内存传递禁入日志；云侧错误细节不进响应仅日志
- 会话 operator 标识来源（scheduler/manual）
- 撤销/非 issued 实例永不入账

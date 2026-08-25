---
feature: "cert-cloud-discovery-import"
journey: "duplicate-concurrent-mapping"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-cloud-discovery-import/proposal.md
generated: "2026-08-25"
---

# Journey: duplicate-concurrent-mapping

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

重复证书与多账号同证书场景下去重与映射建档的正确性：预览双通道判定"已在台账"，导入撞指纹时转补建映射记 success，多账号仅一条台账记录、按账号各建映射。(Source: proposal.md Key Scenarios "重复执行/并发" + "多账号同证书" + Success Criteria 2/5)

## Setup

- 已有 done 快照，快照引用中存在多账号（或多云）引用同一张证书的条目（指纹相同）
- 台账初始为空或已含该指纹证书记录（按分支设定）
- 操作者具备 OpsEngineer 角色

## Happy Path

### Step 1: 查看预览的双通道已在台账判定

**User Action**: 运维人员打开发现预览，查看各条目 inLedger 标记

**Expected Result**: 台账指纹命中或 CloudCertMapping FindByCloudCertID(cloud,accountKey,cloudCertId) 命中任一通道命中即 inLedger=true 并灰选；未登记条目 inLedger=false 可勾选

### Step 2: 确认导入多账号同证书条目组

**User Action**: 运维人员勾选含两个账号引用同一证书（同指纹）的条目组并确认导入

**Expected Result**: 创建导入会话，两个条目进入逐条处理队列

### Step 3: 首条完成指纹登记与映射建档

**User Action**: 会话处理首条：GetCert → 净化 → 解析 → 指纹登记 → 映射建档

**Expected Result**: 台账新增一条 fingerprint_only 记录（uk_fingerprint 唯一），并为本云本账号建立一条 CloudCertMapping

### Step 4: 次条撞指纹转补建映射

**User Action**: 会话处理同指纹的另一账号条目，登记时命中现有 uk_fingerprint 哨兵（ErrDuplicateFingerprint）

**Expected Result**: 不复用失败路径：GetByFingerprint 取既有证书，继续 Upsert 本云本账号 CloudCertMapping，条目记 success（说明"已在台账，已补建映射"），多账号场景不因此降级

### Step 5: 验证收敛结果

**User Action**: 会话终态后，运维人员刷新台账与映射数据核对

**Expected Result**: 同指纹仅 1 条台账记录；CloudCertMapping 按账号各 1 条（uk_fp_cloud_account 两段去重）；多条 CertReference 关联到该证书；会话终态 completed（无条目降级为失败）

## Edge Cases

### Step 4b: 并发会话同时到达同指纹

**Precondition**: 两个导入会话（或同会话重放与在途会话）并发处理同指纹条目，登记写入竞争

**User Action**: 后到者在写入时捕获 ErrDuplicateFingerprint

**Expected Result**: 走取既有证书补建映射路径，条目记 success；不产生第二条台账记录，无失败条目

### Step 1b: 指纹通道命中而映射通道缺失

**Precondition**: 台账已有该指纹证书（如手工导入过），但本云本账号 CloudCertMapping 不存在

**User Action**: 运维人员查看该条目 inLedger 并（经强制路径）提交导入

**Expected Result**: 预览 inLedger=true（指纹通道命中）；若导入执行则补建映射记 success，台账不重复

### Step 1c: 映射通道命中

**Precondition**: 台账证书曾被云端导入过，CloudCertMapping FindByCloudCertID 已命中（如台账记录后续被修改导致指纹比对不中）

**User Action**: 运维人员查看该条目 inLedger

**Expected Result**: inLedger=true 灰选不可选，双通道任一命中即视为已在台账

### Step 5b: 同一会话重放相同条目

**Precondition**: 首轮导入成功后，运维人员将同一批条目原样再次导入

**User Action**: 提交重放导入并等待终态

**Expected Result**: 不产生重复台账记录与重复映射；全部条目经补建映射路径记 success，幂等收敛 completed

### Step 5c: 不同云同指纹证书

**Precondition**: 阿里云与腾讯云各有一张内容相同（同指纹）的证书被引用

**User Action**: 一次导入勾选两云条目

**Expected Result**: 仅 1 条台账记录，两云各自账号各建 1 条 CloudCertMapping，引用分别关联

### Step 3b: 撞指纹既有证书处于异常态

**Precondition**: ErrDuplicateFingerprint 捕获后 GetByFingerprint 取到的既有证书存在（非删除态）

**User Action**: 会话继续 Upsert 映射

**Expected Result**: 映射建档成功且指向既有证书 ID（mappedCertID 正确），条目状态与原因文案准确

## Journey Invariants

- 台账按指纹全局唯一（uk_fingerprint）：任何路径（顺序/并发/重放）不得产生第二条同指纹台账记录
- CloudCertMapping 按（指纹, 云, 账号）两段去重（uk_fp_cloud_account），同键重放幂等
- inLedger 判定恒为双通道（台账指纹 OR 映射 FindByCloudCertID），任一命中即 true
- 并发/重放撞指纹一律转"取既有证书 + 补建映射 + success"，永不降级为失败条目
- 映射建档后 mappedCertID 指向台账既有证书，引用关联按指纹即时生效

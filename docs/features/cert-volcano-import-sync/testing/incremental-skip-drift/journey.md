---
feature: "cert-volcano-import-sync"
journey: "incremental-skip-drift"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-volcano-import-sync/proposal.md
generated: "2026-09-17"
---

# Journey: incremental-skip-drift

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

已有台账下的增量同步正确性：已映射指纹跳过控制同步成本、映射缺失补建、同 cloudCertID 云端重签发（换证漂移）的新映射刷新与旧映射留痕、反查恒取最新指纹。(Source: proposal.md Key Scenario "云端续期换证" + Proposed Solution 冲突策略表 + Success Criteria 2 + Innovation Highlights)

## Setup

- 台账已含若干实例指纹与映射（非首轮）
- 火山侧同 (cloud,accountKey,cloudCertID) 内容被云端重签发（新指纹）
- 部分实例台账有指纹但映射缺失（漂移态）

## Happy Path

### Step 1: 同步轮比对识别增量集

**User Action**: 触发一轮同步（定时或手动），判定层以实例清单元数据 vs 台账指纹+现有映射比对

**Expected Result**: 仅「指纹不在台账」或「映射缺失/漂移」实例进入导入动作，其余归入跳过集

### Step 2: 已映射实例跳过

**User Action**: 判定层处理已映射指纹实例

**Expected Result**: 跳过：无导入动作、无台账/映射写

### Step 3: 映射缺失实例补建

**User Action**: 判定层处理台账有指纹但本云本账号映射缺失的实例

**Expected Result**: 仅补建映射记 success（台账不重复）

### Step 4: 换证漂移实例刷新

**User Action**: 判定层处理同 cloudCertID 新指纹实例：新指纹入台账

**Expected Result**: 新指纹写入台账，并新建映射行（刷新语义）

### Step 5: 旧映射留痕与反查取最新

**User Action**: 核对旧指纹映射与按云证书 ID 反查

**Expected Result**: 旧指纹映射留痕不删；反查按 uploadedAt 降序取最新（指向新指纹）；会话记录漂移事件

### Step 6: 会话收敛核对

**User Action**: 等待终态核对台账/映射/会话逐项结果

**Expected Result**: 台账新旧指纹各 1 条；映射新行为最新；Items[].result 含漂移标注；会话收敛无失败（漂移不是失败）

## Edge Cases

### Step 1b: 全量已映射集合零动作

**Precondition**: 全部实例已映射（无任何增量）

**User Action**: 一轮同步

**Expected Result**: 零导入动作、零台账/映射写；五云口径零材料通道 Get

### Step 2b: 火山口径跳过语义

**Precondition**: 火山实例已入账且映射完整

**User Action**: 一轮同步处理该实例

**Expected Result**: 跳过=不导入不写台账/映射（List 内逐实例 Get 为适配器固有成本，不作跳过收益断言口径）

### Step 3b: 补建撞唯一键重放

**Precondition**: 映射已补建后重放同实例

**User Action**: 再处理同实例

**Expected Result**: 唯一键幂等，不产生第二行，仍记 success

### Step 4b: 新指纹与库内既有证书撞指纹

**Precondition**: 重签发内容与台账另一证书同指纹

**User Action**: 处理该实例

**Expected Result**: ErrDuplicateFingerprint 幂等转补建映射记 success，不产生第二条台账

### Step 4c: 重签发后实例被撤销

**Precondition**: 新指纹实例 IsCertificateRevoked 或非 issued

**User Action**: 处理该实例

**Expected Result**: 不入账，会话留痕，旧映射维持留痕态

### Step 5b: 多历史映射反查次序

**Precondition**: 同 cloudCertID 存在多条历史映射（多次换证）

**User Action**: 按云证书 ID 反查

**Expected Result**: 按 uploadedAt 降序取最新，指向最新指纹

### Step 5c: 旧映射不主动清理

**Precondition**: 换证漂移完成后

**User Action**: 检查旧映射行

**Expected Result**: 留痕不删（孤儿回收 Out of Scope，属清理域独立任务）

### Step 6b: 漂移事件可观测

**Precondition**: 本轮含漂移实例

**User Action**: 查看会话逐项结果

**Expected Result**: 该实例 result 标注漂移/补刷语义，operator 标识来源

## Journey Invariants

- 映射唯一键=certFingerprint+cloud+accountKey（非 cloudCertID）
- 旧指纹映射只留痕不删；反查恒取 uploadedAt 最新
- 已映射指纹跳过：判定层不发起导入动作、不写台账/映射
- 任意重复轮幂等：不产生重复台账/映射行
- 漂移是刷新语义不是失败语义（会话无 failed 条目）

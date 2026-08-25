---
feature: "cert-cloud-discovery-import"
journey: "placeholder-fingerprint-backfill"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-cloud-discovery-import/proposal.md
generated: "2026-08-25"
---

# Journey: placeholder-fingerprint-backfill

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

占位指纹引用（扫描时无法解析指纹，如腾讯 SHA-1 口径）经导入回填真实指纹后关联：预览标记"导入时解析"，导入成功即按（cloud, accountKey, cloudCertId）批量回填并即时关联，真实指纹引用永不被覆盖。(Source: proposal.md Key Scenario "占位指纹引用" + Success Criterion 6)

## Setup

- 已有 done 快照，快照引用中存在占位指纹引用（占位公式 certscan-unresolved:{cloud}|{accountKey}|{certId}，验收样本含腾讯 SHA-1 回退例）
- 对应 cloudCertId 的证书在云侧仍存在且 GetCert 可返回可净化 PEM
- 台账初始为空，操作者具备 OpsEngineer 角色

## Happy Path

### Step 1: 查看占位条目的"导入时解析"标记

**User Action**: 运维人员打开发现预览，定位占位指纹条目

**Expected Result**: 该类条目在预览中被标记"导入时解析"，可勾选（区别于华为云/AWS IAM-hosted 的不可选组）

### Step 2: 确认导入含占位条目的清单

**User Action**: 运维人员勾选含占位指纹条目的清单并确认导入

**Expected Result**: 创建导入会话，占位条目进入逐条处理队列

### Step 3: 导入时解析真实指纹并登记

**User Action**: 会话处理占位条目：GetCert 取 PEM → 仅 CERTIFICATE 块净化 → 解析出真实指纹 → 指纹登记

**Expected Result**: 解析成功，台账新增 fingerprint_only 记录并建 CloudCertMapping，条目记 success

### Step 4: 触发占位引用批量回填

**User Action**: 该条目成功后，会话按（cloud, accountKey, cloudCertId）将 cert_references 中仍为占位指纹的引用批量回填为真实指纹

**Expected Result**: 匹配三元组的占位引用全部回填为导入时点 GetCert 解析的真实指纹（回填语义=导入时点该 cloudCertId 对应的现行证书）；回填后引用按指纹即时关联生效；真实（非占位）指纹引用不受影响

### Step 5: 核对台账引用列表

**User Action**: 运维人员打开新登记证书的台账详情查看引用列表

**Expected Result**: 引用列表非空——回填后的占位引用与真实指纹引用均关联到该证书（四云引用关联即时生效；华为云与不可解析占位引用不在本路径）

## Edge Cases

### Step 3b: 占位条目导入时解析失败

**Precondition**: 导入时点 GetCert 返回的 PEM 无法解析出指纹（或 GetCert 失败）

**User Action**: 会话处理该条目

**Expected Result**: 条目记因失败、绝不触发回填；失败不污染会话语义（partial_failed 既有语义），后续条目继续处理

### Step 4b: 同一 cloudCertId 存在真实指纹引用

**Precondition**: 同一（cloud, accountKey, cloudCertId）下既有占位指纹引用又有真实（非占位）指纹引用

**User Action**: 会话执行回填

**Expected Result**: 仅占位指纹引用被回填；真实指纹引用永不被回填覆盖（续期漂移只留下可由重扫刷新的覆盖率缺口）

### Step 4c: ACM 续期保留 ID 的现行证书口径

**Precondition**: AWS ACM 证书续期后保留同一证书 ID/ARN 但内容已更换，扫描时点引用为占位指纹

**User Action**: 导入时点执行回填

**Expected Result**: 回填的是导入时点 GetCert 得到的现行证书指纹（非扫描时点旧内容），符合"回填一律以导入时点为准"语义

### Step 4d: 误回填可由重扫恢复

**Precondition**: 假设回填写入了错误指纹

**User Action**: 触发一次新的引用扫描

**Expected Result**: 占位指纹是确定性可重算值（按 certscan-unresolved:{cloud}|{accountKey}|{certId} 公式由引用三元组重得），重扫可按原口径重建占位引用，误回填可恢复

### Step 4e: 多账号占位引用批量回填

**Precondition**: 同一 cloudCertId 被多个账号的引用以占位指纹引用（各自三元组不同）

**User Action**: 各账号条目分别导入成功

**Expected Result**: 每个成功的（cloud, accountKey, cloudCertId）三元组各自触发批量回填，仅命中本三元组的占位引用被回填，不跨账号误写

### Step 5b: 部分占位引用解析失败后重扫刷新

**Precondition**: 首轮导入部分占位条目解析失败未回填，快照已陈旧

**User Action**: 运维人员重扫后重跑导入

**Expected Result**: 重扫按原口径重建占位引用，重跑导入对剩余条目幂等处理，回填最终收敛

## Journey Invariants

- 非占位（真实）指纹引用永不被回填覆盖
- 回填仅由导入会话按条目成功事件承担：不动扫描编排、不改 cert_references 表结构
- 回填一律以导入时点 GetCert 结果为准（现行证书口径）
- 解析失败的条目绝不触发回填
- 占位指纹保持确定性可重算（certscan-unresolved 公式），误回填可由重扫按原口径重建

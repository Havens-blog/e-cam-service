---
feature: "cert-cloud-discovery-import"
journey: "unsupported-entries-skip"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-cloud-discovery-import/proposal.md
generated: "2026-08-25"
---

# Journey: unsupported-entries-skip

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

不可解析与云侧已失联条目的跳过语义：华为云整组与 AWS IAM-hosted 条目在预览归入不可选组，导入请求仍含该类条目时逐条记因跳过，绝不阻塞其余条目，失败原因以静态文案呈现。(Source: proposal.md Key Scenarios "云侧证书已删除" + "华为云引用" + Key Risks "AWS IAM-hosted" + Success Criterion 7)

## Setup

- 已有 done 快照，引用中混有：华为云条目（无 PEM 能力）、AWS IAM-hosted（非 ARN 形态）条目、正常可解析条目（阿里/腾讯/Azure/AWS ARN 形态）
- 正常条目对应的云侧证书仍存在
- 操作者具备 OpsEngineer 角色

## Happy Path

### Step 1: 查看预览不可选组

**User Action**: 运维人员打开发现预览，查看华为云条目组与 AWS IAM-hosted 条目

**Expected Result**: 华为云条目整组标记"该云暂不支持自动解析"不可选；AWS IAM-hosted（非 ARN）条目同语义降级不可选；不可解析标记统一由可解析标记字段承载（parseable=false 归入不可选组）

### Step 2: 确认导入混合清单

**User Action**: 运维人员确认导入（清单同时含可解析条目与被强制提交的不可解析条目）

**Expected Result**: 创建导入会话，全部提交条目进入逐条处理队列，会话正常启动

### Step 3: 不可解析条目逐条记因跳过

**User Action**: 会话处理不可解析条目：华为云无 PEM 能力、AWS IAM-hosted GetCert 显式报错不支持、云侧已删除条目 GetCert Exists=false

**Expected Result**: 该类条目各自记因跳过（如"云侧已不存在"、"暂不支持"），不产生任何台账/映射写入，也不阻塞后续可解析条目的正常导入

### Step 4: 查看终态与逐条原因

**User Action**: 运维人员查看会话终态与逐条 errorReason

**Expected Result**: 会话收敛 completed/partial_failed（跳过条目按既有语义计入失败侧但不中断会话）；每条 errorReason 为静态文案，不携带云响应片段与凭证信息；可解析条目全部正常登记

## Edge Cases

### Step 3b: 云侧证书在预览后被删除

**Precondition**: 预览时条目可解析且可选，确认导入前云侧证书已被删除

**User Action**: 会话处理该条目时实时 GetCert 校验

**Expected Result**: GetCert Exists=false，记因"云侧已不存在"跳过，不阻塞其余条目，预览明确标注"基于快照时点"以界定责任

### Step 3c: 华为云条目被强制提交

**Precondition**: 前端灰选防护被绕过（如直接构造导入请求含华为云条目）

**User Action**: 导入请求含华为云条目

**Expected Result**: 服务端逐条记因跳过该类条目（华为云无 PEM 能力），不产生台账写入，其余条目正常处理

### Step 3d: AWS IAM-hosted 证书 ID 显式报错

**Precondition**: 条目为 AWS IAM-hosted（非 ARN 形态）证书 ID，GetCert 对该形态显式报错不支持

**User Action**: 会话处理该条目

**Expected Result**: 记因"暂不支持"跳过（与华为云同组语义），不阻塞其余条目；预览侧该条目本应 parseable=false 不可选

### Step 4b: 全部条目均不可解析

**Precondition**: 勾选提交的条目全部为不可解析类（华为云/IAM-hosted/云侧已删除）

**User Action**: 运维人员等待会话终态

**Expected Result**: 会话收敛终态（全部条目记因），不产生任何台账/映射/引用回填写入，无静默丢失

### Step 4c: 失败文案泄漏检查

**Precondition**: 某条目失败于云 API 返回异常内容（含响应片段或凭证信息的可能）

**User Action**: 运维人员查看该条 errorReason

**Expected Result**: errorReason 恒为静态文案，不携带云响应片段、凭证或内部错误细节

## Journey Invariants

- 不可解析/不可选条目绝不产生台账写入、映射建档或引用回填
- 任何单条跳过不阻塞会话中其余条目的处理
- errorReason 恒为静态文案，不泄漏云响应内容与凭证
- 不可选判定统一由可解析标记字段承载（parseable=false 归入不可选组），预览与导入两侧口径一致
- 导入时逐条实时 GetCert 校验 Exists，以导入时点云侧状态为准

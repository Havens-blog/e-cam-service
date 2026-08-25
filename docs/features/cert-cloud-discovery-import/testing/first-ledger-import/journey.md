---
feature: "cert-cloud-discovery-import"
journey: "first-ledger-import"
risk_level: "High"
golden_path: true
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-cloud-discovery-import/proposal.md
generated: "2026-08-25"
---

# Journey: first-ledger-import

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

运维人员在空台账上完成存量证书首次登记：以最近一次引用扫描快照为数据源，预览-确认两步完成云端发现导入，部分失败后重跑收敛，让台账从空置进入可用状态。(Source: proposal.md Key Scenario "首次登记（主路径）" + "Proposed Solution 用户旅程" + Success Criteria 1/4/5)

## Setup

- 台账当前为空（无任何证书记录）
- 已存在一次 status=done 的引用扫描快照，快照内含阿里云/腾讯云/Azure/AWS 四云可解析引用（可能混杂华为云与占位指纹引用）
- 操作者具备 OpsEngineer 角色

## Happy Path

### Step 1: 发起云端发现预览

**User Action**: 运维人员在台账页空态点击"从云端导入存量证书" CTA（或工具栏按钮），请求发现预览

**Expected Result**: 系统基于最近 done 快照纯 DB 聚合生成唯一证书清单（按 cloud+accountKey+cloudCertId 去重，排除 product=crd 引用与空 cloud 条目），响应耗时 < 1s 且不调用任何云 API

### Step 2: 查看预览清单与快照时点

**User Action**: 运维人员浏览预览列表，核对每个条目的云/账号/云证书 ID/引用资源数/inLedger 标记/notAfter/可解析标记七类字段及快照时间 snapshotStartedAt

**Expected Result**: 每个条目含七类字段；未登记条目 notAfter 显示"—（导入后补全）"占位；已在台账条目 inLedger=true 显示台账 NotAfter；响应另含 snapshotStartedAt

### Step 3: 勾选并确认导入

**User Action**: 运维人员接受默认勾选（全部未登记条目，已在台账条目灰选不可改），点击确认导入

**Expected Result**: 系统创建发现导入会话（202 语义）并持久化后异步执行，浏览器可安全关闭

### Step 4: 会话逐条处理导入条目

**User Action**: 运维人员等待会话逐条执行：GetCert 取公钥 PEM → 仅 CERTIFICATE 块净化 → 解析 → 指纹登记（fingerprint_only）→ CloudCertMapping 幂等建档

**Expected Result**: 四云证书经净化拼装（叶在前 fullchain，AWS 含 CertificateChain 拼接）后解析成功，台账新增 fingerprint_only 记录并建立云证书映射；成功条目触发占位指纹引用回填；单条失败记 errorReason 不中断后续条目

### Step 5: 轮询进度至终态

**User Action**: 运维人员在导入进度界面（复用批量导入会话进度交互）轮询会话进度直至终态

**Expected Result**: 会话收敛到 completed/partial_failed 终态；partial_failed 时逐条失败原因（静态文案）可见

### Step 6: 重跑处理剩余失败项

**User Action**: 针对部分失败结果，运维人员重新发起导入，仅勾选上轮失败条目

**Expected Result**: 重跑仅处理剩余项且幂等：已成功条目不产生重复台账记录，最终台账与映射收敛到一致状态，刷新台账页可见新登记证书及其引用关联

## Edge Cases

### Step 1b: 无 done 快照时请求预览

**Precondition**: 系统不存在任何 status=done 的扫描快照

**User Action**: 运维人员请求发现预览

**Expected Result**: 返回 NO_SNAPSHOT 结构化错误码（非 500），前端进入"先执行扫描"引导流程（详见 no-snapshot-guidance journey）

### Step 2b: 快照陈旧超 7 天

**Precondition**: 最近 done 快照的 snapshotStartedAt 距当前超过 7 天

**User Action**: 运维人员查看预览响应携带的 snapshotStartedAt

**Expected Result**: 前端显著提示快照超期建议重扫；预览条目明确标注"基于快照时点"（不承诺云侧现状）

### Step 2c: 条目属于不可解析组

**Precondition**: 预览清单中混有华为云条目或 AWS IAM-hosted（非 ARN）条目

**User Action**: 运维人员查看该类条目的可解析标记

**Expected Result**: 该类条目 parseable=false 归入不可选组（华为云整组标记"该云暂不支持自动解析"，AWS IAM-hosted 同语义降级），不可勾选

### Step 3b: 已在台账条目尝试勾选

**Precondition**: 条目经双通道判定（台账指纹命中或 CloudCertMapping FindByCloudCertID 命中）inLedger=true

**User Action**: 运维人员尝试修改已在台账条目的勾选状态

**Expected Result**: 该类条目灰选不可操作，不会被纳入导入请求

### Step 4b: 单条导入失败不中断会话

**Precondition**: 会话中某条目在导入过程中失败（如占位指纹条目解析失败、云侧证书已删除）

**User Action**: 运维人员观察会话继续处理后续条目

**Expected Result**: 失败条目记录 errorReason（静态文案，不携带云响应片段），会话不中断，其余条目正常收敛

### Step 4c: 云侧证书在预览后被删除

**Precondition**: 预览生成后、导入执行前，云侧该证书已被删除（预览与导入间状态漂移）

**User Action**: 会话处理该条目时实时 GetCert 校验

**Expected Result**: GetCert Exists=false，该条记因"云侧已不存在"跳过，不阻塞其余条目

### Step 5b: 浏览器中断后重开进度

**Precondition**: 导入会话执行中浏览器被关闭或刷新

**User Action**: 运维人员重新打开台账页与导入入口

**Expected Result**: 会话已先持久化再异步执行，重开后进度可续看，不丢结果

### Step 5c: 大账号证书量导致会话超时

**Precondition**: 勾选条目数量大，逐证限速调用云 API 使会话逼近整体限时（对齐批量导入 10 分钟口径）

**User Action**: 运维人员等待会话终态

**Expected Result**: 超时条目记因可重跑（幂等），会话分批记录进度，不静默丢失已处理结果

## Journey Invariants

- 私钥全程不落库不入日志：入库 CertPEM 含且仅含 CERTIFICATE 块（叶在前 fullchain），不含 "PRIVATE KEY" 字样；EncryptedPrivateKey 为空、hostingStatus=fingerprint_only；净化前原始 buffer 用后 Zeroize
- 预览端点始终纯 DB 聚合（快照引用 + 台账指纹/映射比对），绝不调用云 API，响应 < 1s
- 台账记录按指纹全局唯一（uk_fingerprint），任何重跑/并发路径不产生重复台账记录
- 单条失败或 panic 不中断导入会话（对齐批量导入 Hard Rule）
- 云凭证仅内存使用禁入日志（沿用扫描链路约束）
- 全部发现端点（预览/导入/进度）对非 OpsEngineer 角色一律 403

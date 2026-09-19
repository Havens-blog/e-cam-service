---
feature: "cert-volcano-deployer"
journey: "volcano-product-aware-upload"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-volcano-deployer/proposal.md
generated: "2026-09-19"
notes: "新增可测面（功能级 tests/ 未覆盖）：ProductAwareUploader 产品感知上传可选端口。本 journey 为补充性测试文档。"
---

# Journey: volcano-product-aware-upload

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

火山四产品证书库独立（CDN/WAF/ALB-NLB/certificateservice），`UploadCert` 按 `ProductAwareUploader` 可选端口产品定向：调用方无需预知产品库差异，部署器按资源产品路由到对应上传 API，产物即该产品库可绑定证书，云证书 ID 以 `{product}:{id}` 前缀归一。关键判断：各产品独立证书库不阻塞两段式——按产品上传即得该库 ID。该端口为可选（可选端口缺失时回退通用上传语义不报错），csv fail-fast 语义已在实现中消解。(Source: proposal.md Proposed Solution 1 + Innovation Highlights + Assumptions Challenged 首行；写操作 + 云证书库状态变更故定级 High)

## Setup

- 火山部署器已装配（volcano×4 产品），证书束（证书+链+私钥）在台账就绪
- 火山证书库可用（fake SDK 桩承载四产品库独立形态）
- 上传命名助手（`deployer_common.go` generateUploadName）可复用

## Happy Path

### Step 1: CDN 产品定向上传

**User Action**: 对火山 CDN 引用执行两段式第一段上传

**Expected Result**: 经 `AddCdnCertificate` 上传至 CDN 证书库，返回云证书 ID 归一形态 `cdn:{id}`

### Step 2: WAF 产品定向上传

**User Action**: 对火山 WAF 域名引用执行第一段上传

**Expected Result**: 上传为 WAF 服务证书，云证书 ID 归一形态 `waf:{id}`

### Step 3: ALB/NLB 产品定向上传

**User Action**: 对火山 ALB / NLB 监听引用分别执行第一段上传

**Expected Result**: 监听证书上传至对应产品库，云证书 ID 归一形态 `alb:{id}` / `nlb:{id}`

### Step 4: 可选端口缺失回退

**User Action**: 以未实现 `ProductAwareUploader` 端口的部署器实例执行上传路径

**Expected Result**: 可选端口缺失不报错，回退通用上传语义（非产品定向路径行为可预期），调用方无感知

## Edge Cases

### Step 1b: 上传命名冲突幂等

**Precondition**: 同一证书束对同一产品重复触发上传

**User Action**: 再次执行第一段

**Expected Result**: generateUploadName 幂等命名，云证书库不产生语义重复条目（按云侧语义去重或新条目可被映射收敛），映射不重复落行

### Step 2b: 证书链缺根回退

**Precondition**: 证书束链缺根（火山侧校验会拒）

**User Action**: 执行上传

**Expected Result**: checkChain 系统信任库回退（依赖 `644b067`）先消解可补链场景；仍不可信则结构化失败 + 静态 reason

### Step 3b: 私钥缺失不可上传

**Precondition**: 台账证书 fingerprint_only

**User Action**: 执行上传路径

**Expected Result**: 上传前拦截（清单生成评估语义），不产生孤儿库条目

### Step 3c: 四产品 ID 空间互斥

**Precondition**: 同一证书束已分别上传至四个产品库

**User Action**: 核对四条云证书 ID

**Expected Result**: `{product}:{id}` 前缀互斥、同库内 id 不冲突；跨产品不误引（ID 归一断言口径，对齐三云 ID 空间互斥测试先例）

### Step 4b: 可选端口部分实现

**Precondition**: 部署器仅对部分产品实现产品感知上传

**User Action**: 对已实现/未实现产品分别触发上传

**Expected Result**: 已实现产品走定向路径，未实现产品走回退路径，两者产物映射形态一致（同 `{product}:{id}` 归一）

## Journey Invariants

- 云证书 ID 恒为 `{product}:{id}` 前缀归一，产品库 ID 空间互斥
- ProductAwareUploader 为可选端口：缺失不阻塞、不报错，回退通用上传语义
- 上传失败不产生悬空绑定（两段式保证：先上传成功落映射 active 才进入 BindResource）
- 上传名称幂等，重复上传不产生重复映射
- 私钥明文仅内存传递、用后 Zeroize；云侧错误细节仅日志

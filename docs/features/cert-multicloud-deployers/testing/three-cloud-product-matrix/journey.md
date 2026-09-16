---
feature: "cert-multicloud-deployers"
journey: "three-cloud-product-matrix"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-multicloud-deployers/proposal.md
generated: "2026-09-16"
---

# Journey: three-cloud-product-matrix

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

运维人员对三云全部 9 个 cloud×product 组合（华为 cdn/waf/alb/nlb + AWS cdn/alb/nlb + Azure cdn/alb）逐一执行证书替换：各组合在变更清单中均为可执行项，云证书 ID 形态（SCM ID / ACM ARN / KV 引用）与各产品绑定机制差异（CloudFront 跨地域、NLB 监听证书、App Gateway 经 KV 引用）由每云适配层归一，用户全程无感差异。(Source: proposal.md Key Scenario "边界" + Scope "In Scope" 第 1-5 条 + Success Criteria 1/2/3)

## Setup

- 三云部署器已装配并注册 9 个 cloud×product 组合，`discoveryOnlyClouds` 已移除三云
- 扫描适配已产出 9 个组合的引用记录（各组合至少一条待替换证书引用）
- 三云账号凭证与前置资源就绪（AWS 账号可用、Azure 已预置 Key Vault 实例）

## Happy Path

### Step 1: 生成覆盖 9 组合的变更清单

**User Action**: 运维人员生成覆盖三云全部产品引用的变更清单

**Expected Result**: 9 个 cloud×product 组合的引用均为可执行项（AutoChangeable=true），无一组合按 ERR_DISCOVERY_ONLY 标记 skipped

### Step 2: 华为云四产品执行两段式

**User Action**: 运维人员对华为云 cdn/waf/alb/nlb 四组合执行上传与绑定

**Expected Result**: 证书上传至 SCM 返回 SCM ID，四产品分别绑定成功，SCM ID 形态写入 CloudCertMapping

### Step 3: AWS CloudFront 执行两段式（us-east-1 约束）

**User Action**: 运维人员对 AWS cdn（CloudFront）组合执行上传与绑定

**Expected Result**: 证书经 ImportCertificate 固定上传至 us-east-1 地域 ACM，返回 ACM ARN，CloudFront 分配引用成功，ARN 形态写入映射

### Step 4: AWS ALB/NLB 执行两段式（绑定 API 分支）

**User Action**: 运维人员对 AWS alb/nlb 两组合执行上传与绑定

**Expected Result**: ALB 走绑定 API、NLB 走监听证书 API，各自绑定成功；两组合均落 ACM ARN 映射

### Step 5: Azure 两产品执行两段式（KV 引用绑定）

**User Action**: 运维人员对 Azure cdn（Front Door）/alb（Application Gateway）两组合执行上传与绑定

**Expected Result**: 证书上传至 Key Vault 产生 KV secret ID 引用；Front Door 直接绑定、App Gateway 经 KV 证书名称引用绑定，均成功并落 KV 引用形态映射

### Step 6: 全组合映射与清单终态核对

**User Action**: 运维人员核对 9 组合的 CloudCertMapping 记录与变更清单终态

**Expected Result**: 9 组合映射齐备且形态按云正确（SCM ID / ACM ARN / KV 引用），无跨云混淆；清单终态与执行结果一致

## Edge Cases

### Step 1b: 某组合发现引用为空

**Precondition**: 9 组合中某产品当前无任何证书引用

**User Action**: 运维人员生成变更清单

**Expected Result**: 该组合跳过不产出条目，不影响其余组合正常产出与执行；不报错不误标

### Step 2b: 华为 SCM 证书 ID 形态异常

**Precondition**: 华为云上传返回的证书 ID 不符合 SCM ID 预期形态

**User Action**: 系统尝试归一并写入映射

**Expected Result**: 显式失败不猜测：该组合条目失败且原因明确，不写脏映射，其余组合不受影响

### Step 3b: CloudFront 证书误入非 us-east-1 地域

**Precondition**: AWS 账号默认区域非 us-east-1

**User Action**: 系统对 CloudFront 组合执行上传段

**Expected Result**: UploadCert 固定使用 us-east-1 地域（参照 CAS 地域固定模式），不依赖账户默认区域，上传与分配成功

### Step 4b: NLB 误用 ALB 绑定 API

**Precondition**: NLB 组合的绑定被错误路由到 ALB 绑定 API

**User Action**: 系统对 NLB 组合执行绑定段

**Expected Result**: 按产品分支走监听证书 API，绑定成功且行为与 ALB 分支可区分；不出现"调用成功实则无效"的假绑定

### Step 5b: Azure App Gateway 拒绝直传证书 ID 绑定

**Precondition**: App Gateway 组合被按"直接上传 ID"语义绑定

**User Action**: 系统对 App Gateway 组合执行绑定段

**Expected Result**: 按 KV 证书名称引用语义绑定成功；引用指向本次上传的 KV 证书而非任意资源 ID

### Step 5c: Azure Key Vault 实例未预置

**Precondition**: Azure 账号下不存在可用 Key Vault 实例

**User Action**: 系统对 Azure 组合执行上传段

**Expected Result**: 显式失败并提示前置资源缺失（Key Vault 需预先存在），不猜测默认实例、不静默降级；其余云组合不受影响

### Step 6b: 组合间映射形态互斥校验

**Precondition**: 核对映射时尝试将某云 ID 记录到另一云条目（如把 ACM ARN 记到华为条目）

**User Action**: 系统写入/查询映射

**Expected Result**: 三云 ID 空间互斥，跨云混用被识别并拒绝，映射唯一性保持（无需云内账户级消歧）

## Journey Invariants

- 9 个 cloud×product 组合共用同一五方法端口语义与两段式编排，云差异只在每云适配层归一
- 云证书 ID 形态按云固定：华为 SCM ID、AWS ACM ARN、Azure KV secret ID 引用；形态校验失败显式报错不猜测
- CloudFront 组合上传地域恒为 us-east-1，不受账户默认区域影响
- 单组合失败隔离：任一组合的失败/跳过不影响其余组合的执行与终态
- 私钥明文仅内存传递、用后 Zeroize；云端错误细节不进响应仅日志

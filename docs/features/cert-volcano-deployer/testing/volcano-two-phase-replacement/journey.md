---
feature: "cert-volcano-deployer"
journey: "volcano-two-phase-replacement"
risk_level: "High"
golden_path: true
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-volcano-deployer/proposal.md
generated: "2026-09-19"
existing_tests:
  - tests/volcano-cert-replacement/volcano_cert_replacement_test.go::TestVolcanoCertReplacement_FullLifecycleSmoke
---

# Journey: volcano-two-phase-replacement

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

火山证书替换的 Golden Path：火山引用到期 → 引用扫描发现 → 变更清单生成（AutoChangeable=true）→ 确认分批灰度 → 两段式执行（产品定向 UploadCert → 映射 active → BindResource）→ 验证窗口 TLS 拨测 → 旧证书孤儿清理。火山与五云同一状态机、同一两段式编排，差异仅在证书库形态（四产品库独立 → 第一段按产品定向上传，产物即该库可绑定证书）。(Source: proposal.md Key Scenario "Happy path" + "产品分支" + Success Criteria 3/4/6；全程状态变更与云侧写操作故定级 High)

## Setup

- 火山账号已登记（provider `volcano`/`volcengine`），`module.go` 第 6 云装配完成（volcano×4 产品 deployer + 扫描适配器）
- 证书台账存在 fingerprint 配对（旧证书已到期、新证书含私钥非 fingerprint_only）
- 线上资源：火山 CDN/WAF/ALB/NLB 各有资源引用旧证书指纹
- 火山证书库可用（fake SDK 桩或活体验收）

## Happy Path

### Step 1: 引用扫描发现火山引用并生成变更清单

**User Action**: 引用扫描（只读）枚举火山四产品资源，清单生成评估火山引用可执行性

**Expected Result**: 火山 CDN/WAF/ALB/NLB 引用进入变更清单，逐项 `AutoChangeable=true`（cloud_api 通道），`Target.Cloud=volcano`；不再 `ERR_DISCOVERY_ONLY`/skipped（回归口径）

### Step 2: 确认变更清单并配置分批灰度

**User Action**: 运维确认清单并提交分批策略（批次比例 ≤50%）

**Expected Result**: 清单进入可执行状态，项目按批次划分（如 4 项分 2 批），火山引用按云/产品通道分发命中火山部署器

### Step 3: 执行批次——产品定向第一段上传

**User Action**: 触发批次执行，编排走两段式第一段 `UploadCert`（按产品定向：CDN `AddCdnCertificate` / WAF 服务证书 / ALB-NLB 监听证书 / certificateservice `ImportCertificate`）

**Expected Result**: 上传产物写入云证书库对应产品库，映射落 `active`，云证书 ID 形态 `{product}:{id}` 前缀归一（对齐 huawei SCM / AWS ACM ARN 归一模式）

### Step 4: 执行批次——第二段绑定资源

**User Action**: 编排续走第二段 `BindResource`（CDN=`BatchDeployCert` 加速域名；WAF=域名证书替换；ALB/NLB=监听证书更新）

**Expected Result**: 各产品资源证书引用切换为新证书，item 状态 success；绑定引用上传产物 ID（该产品库可绑定证书）

### Step 5: 验证窗口拨测并人工续批

**User Action**: 非终批进入验证窗口，`ProbeDomains` 按域名 TLS 拨测线上指纹（云无关复用），确认后人工续批

**Expected Result**: 拨测线上指纹 = 新证书指纹；批次按序推进直至终批完成

### Step 6: 旧证书孤儿清理

**User Action**: 终批完成后触发旧证书孤儿清理（`CleanupOrphan`）

**Expected Result**: 旧证书孤儿按映射队列清理（`DeleteInstance` 幂等），台账/映射状态收敛无残留

## Edge Cases

### Step 1b: fingerprint_only 私钥缺失被清单评估拦截

**Precondition**: 台账新证书为 fingerprint_only（无私钥）

**User Action**: 清单生成评估该火山引用

**Expected Result**: 拦截不可执行（既有通用语义，与五云一致），不进入 AutoChangeable 面

### Step 1c: 无指纹通道产品的映射回退指纹解析

**Precondition**: 火山 WAF/ALB/NLB 资源无独立指纹通道

**User Action**: 引用扫描解析该资源指纹

**Expected Result**: 经 GetCert 映射回退解析指纹（映射反查 → GetCert 要素 → 确定性占位指纹 `certscan-unresolved:` 口径），CertReference 指纹不缺失

### Step 2b: 分批比例越界

**Precondition**: 确认时批次比例 >50%

**User Action**: 提交越界分批参数

**Expected Result**: 拦截（灰度上限 ≤50% 语义复用），清单不进入执行态

### Step 3b: 上传失败批次内中断

**Precondition**: 第一段上传被火山证书库拒绝（如证书链缺根且系统信任库回退仍失败）

**User Action**: 执行批次

**Expected Result**: item 失败 + 静态 reason（云侧错误细节仅日志不进响应），同批后续项按编排语义中断，不跨批污染

### Step 4b: 绑定 API 产品差异失败

**Precondition**: 某产品绑定调用失败（如 CDN 域名粒度与 ListReferences 不对齐）

**User Action**: 执行绑定

**Expected Result**: 该 item 结构化失败，其他产品项不受影响；失败触发补偿语义（见 journey volcano-bind-failure-compensation）

### Step 5b: 验证窗口拨测不符

**Precondition**: 验证窗口内线上指纹仍 = 旧指纹（绑定未生效/回滚已发生）

**User Action**: 拨测核对

**Expected Result**: 不推进终批，停留验证窗口可人工介入（回滚路径见 journey volcano-rollback-verify-window）

### Step 6b: 无孤儿时清理幂等空成功

**Precondition**: 首次替换，映射队列无孤儿条目

**User Action**: 触发孤儿清理

**Expected Result**: 空清理幂等成功，无报错无状态污染

### Step 6c: 已成功批次幂等重跑

**Precondition**: 批次已全部 success

**User Action**: 重复触发同批执行

**Expected Result**: 幂等收敛——不重复上传（不产生重复库条目）、不重复绑定、状态同结果

## Journey Invariants

- 火山与五云同一状态机、同一两段式编排（UploadCert→BindResource→失败 CleanupOrphan 补偿）；差异仅在证书库形态与绑定 API
- 云证书 ID 形态恒为 `{product}:{id}` 前缀归一，四产品库 ID 空间互斥不跨库误引
- 分批灰度 ≤50%、验证窗口（ProbeDomains 域名云无关）、回滚、孤儿清理、mapping 全复用零新机制
- 云侧错误细节不进响应仅日志；私钥明文仅内存传递用后 Zeroize
- 引用扫描只读；写操作仅存在于执行面（deployer）

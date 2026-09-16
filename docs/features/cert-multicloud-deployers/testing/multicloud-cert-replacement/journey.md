---
feature: "cert-multicloud-deployers"
journey: "multicloud-cert-replacement"
risk_level: "High"
golden_path: true
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-multicloud-deployers/proposal.md
generated: "2026-09-16"
---

# Journey: multicloud-cert-replacement

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

运维人员对华为云/AWS/Azure 三云的证书引用完成一次与 aliyun/tencent 完全同构的证书替换闭环：三云引用进入变更清单可执行项 → 确认后分批执行两段式（上传证书到云证书库 → 绑定目标资源）→ 云证书 ID 写入映射 → 验证窗口拨测确认线上指纹 → 旧证书孤儿清理，全程无需登录云控制台。(Source: proposal.md Key Scenario "Happy path" + "Proposed Solution 端用户体验" + Success Criteria 1/3/5)

## Setup

- e-cam 已装配三云部署器（华为 cdn/waf/alb/nlb、AWS cdn/alb/nlb、Azure cdn/alb 共 9 个 cloud×product 组合），`discoveryOnlyClouds` 已移除三云
- 三云账号凭证已配置（复用既有 CloudAccount 体系），云证书库可达（华为 SCM / AWS ACM / Azure Key Vault）
- 引用扫描已产出三云资源引用（cdn/waf/alb/nlb 等产品的证书引用记录）
- 存在一张待替换的三云证书（新证书材料已就绪），操作者具备变更执行权限

## Happy Path

### Step 1: 为三云证书引用生成变更清单

**User Action**: 运维人员发起变更清单生成，覆盖华为云/AWS/Azure 的 CDN/ALB/NLB/WAF 证书引用

**Expected Result**: 三云引用进入变更清单可执行项（AutoChangeable=true），不再按 ERR_DISCOVERY_ONLY 分区标记 skipped，与 aliyun/tencent 引用同栏呈现、计入执行成功率分母

### Step 2: 确认变更清单并进入分批执行

**User Action**: 运维人员审阅变更清单后确认，进入分批灰度执行

**Expected Result**: 变更按既有分批策略启动执行；每批对三云引用走与 aliyun/tencent 相同的两段式编排（先上传段、后绑定段）

### Step 3: 上传段——证书上传至各云证书库

**User Action**: 系统代表运维执行 UploadCert，将新证书上传至目标云证书库（华为 SCM / AWS ACM ImportCertificate / Azure Key Vault）

**Expected Result**: 各云返回云证书 ID（华为 SCM ID / AWS ACM ARN / Azure KV secret ID 引用）；CloudFront 目标固定使用 us-east-1 地域的 ACM

### Step 4: 绑定段——绑定目标资源

**User Action**: 系统执行 BindResource，将云证书库中的新证书绑定到引用的目标资源

**Expected Result**: 目标资源（华为 CDN/WAF/ALB/NLB、AWS CloudFront/ALB/NLB、Azure Front Door/Application Gateway）按各自绑定 API 完成证书绑定，绑定结果显式成功或失败

### Step 5: 云证书 ID 写入 CloudCertMapping

**User Action**: 系统将 UploadCert 产生的云证书 ID 按云归一形态写入 CloudCertMapping

**Expected Result**: 映射记录落库且状态为 active，ID 形态按云区分（SCM ID / ACM ARN / KV 引用），三云 ID 空间互斥不混淆

### Step 6: 验证窗口拨测确认线上指纹

**User Action**: 运维人员在验证窗口内等待系统对目标域名执行 TLS 拨测（ProbeDomains，按域名云无关）

**Expected Result**: 拨测取得线上证书指纹，与新证书指纹一致，变更条目标记验证通过

### Step 7: 旧证书孤儿清理

**User Action**: 运维人员确认验证通过后，执行旧证书清理

**Expected Result**: 已无引用的旧云证书进入清理队列并被删除，变更闭环完成；全程未登录任何云控制台

## Edge Cases

### Step 1b: 引用属 K8s 托管资源

**Precondition**: 引用目标资源为 K8s 控制器托管资源（ingress/secret 由控制器调谐）

**User Action**: 运维人员生成包含该引用的变更清单

**Expected Result**: 该引用仍不可自动变更（唯一不可执行来源是 K8s 管理权/托管资源），不入执行分母，引导走控制器/CRD 更新；同清单其余三云引用正常可执行

### Step 2b: 某一批次执行失败中断分批

**Precondition**: 分批执行中某一批失败（如云 API 限流）

**User Action**: 运维人员观察执行进度并续跑剩余批次

**Expected Result**: 已完成批次结果保留，失败批次可重试续跑，不整单回退、不重复执行已完成段

### Step 3b: 上传段成功但绑定段未开始即中断

**Precondition**: UploadCert 已返回云证书 ID，进程在 BindResource 前中断

**User Action**: 运维人员重跑或等待补偿链路收敛

**Expected Result**: 已上传未绑定的云证书不残留为悬空资源：经补偿/清理链路（CleanupOrphan + 映射 orphan 入清理队列）收敛，重跑时重新走两段式而非复用悬空 ID

### Step 4b: AWS NLB 监听证书与 ALB 绑定 API 分支

**Precondition**: 目标资源为 AWS NLB（监听证书 API 与 ALB 绑定 API 不同）

**User Action**: 系统对 NLB 引用执行绑定段

**Expected Result**: 按产品分支选择监听证书 API 完成绑定，不误用 ALB 绑定 API，绑定结果可区分产品语义

### Step 4c: Azure Application Gateway 经 KV 证书名称引用绑定

**Precondition**: 目标资源为 Azure App Gateway（经 Key Vault 证书名称引用而非直接上传 ID 绑定）

**User Action**: 系统对 App Gateway 引用执行绑定段

**Expected Result**: 按 KV 证书引用形态完成绑定（引用而非直传 ID），绑定成功且引用指向本次上传的证书

### Step 5b: 云证书 ID 形态归一失败

**Precondition**: 某云返回的证书 ID 不符合预期形态（如 ACM ARN 解析失败）

**User Action**: 系统尝试将异常形态 ID 写入映射

**Expected Result**: 显式失败不猜测：变更条目失败并给出明确原因，不写入脏映射，不进入验证窗口

### Step 6b: 验证窗口拨测指纹不一致

**Precondition**: 绑定已完成但线上拨测指纹 ≠ 新证书（如 CDN 边缘未同步）

**User Action**: 系统在验证窗口内继续拨测判定

**Expected Result**: 变更不标记验证通过，窗口内重试或超时后进入失败/回滚可用状态；不因单次拨测失败误判成功

### Step 7b: 旧证书仍被其他资源引用

**Precondition**: 待清理的旧云证书仍被清单外其他资源引用

**User Action**: 系统执行孤儿清理判定并删除

**Expected Result**: 仍被引用的旧证书不删除（孤儿判定），保留待后续清理；仅删除确认无引用的旧证书

## Journey Invariants

- 三云替换语义与 aliyun/tencent 完全同构：失败状态、回滚、清理走同一状态机，不引入云特定的新验证/回滚机制
- 两段式顺序不可逆：UploadCert 成功前不得 BindResource；绑定失败必经 CleanupOrphan 补偿，不允许跳过补偿直接重试
- 云证书 ID 空间互斥（SCM ID / ACM ARN / KV 引用），映射唯一性天然成立，禁止跨云混用或猜测归一
- 私钥明文仅内存传递、用后 Zeroize；云端错误细节不进 API 响应仅入日志
- 变更清单可执行性唯一受 K8s 管理权/托管资源约束，三云不因云差异降级为 discovery-only

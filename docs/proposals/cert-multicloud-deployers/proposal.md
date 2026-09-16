---
created: "2026-09-15"
author: "Haven"
status: Approved
intent: "new-feature"
---

# Proposal: 多云证书更新部署能力补齐（华为云/AWS/Azure）

## Problem

e-cam 证书替换对 5 朵云中 3 朵（华为云/AWS/Azure）只能「发现引用、标记为不可自动变更」，无法像阿里云/腾讯云那样经变更清单自动完成证书更新——这些云的证书到期替换仍须人工登录各云控制台操作。

### Evidence

- 代码事实：`changelist_generator.go` `discoveryOnlyClouds` 静态名单将 `huawei/aws/azure` 的引用按 `ERR_DISCOVERY_ONLY` 分区（生成变更清单时标 `skipped`，不计入执行成功率分母）；`module.go` 仅 `RegisterDeployer` 了 `aliyun`/`tencent` 两个部署器。
- 扫描覆盖面已就绪：`reference_scan_service.go` 的发现适配器对华为云（cdn/waf/alb/nlb）、AWS（cdn/alb/nlb）、Azure（cdn/alb）产出引用——「能扫到」但「不能换」。
- 运维日志显示华为云存在真实资源（`hw-jlc-fat-k8s-cce-*`、`hw-jlc-dev-k8s-cce-*` 安全组），三云非空环境。

### Urgency

- 证书到期替换是三云运维高频动作；当前三云引用在变更清单中恒定 `skipped`，运营只能人工更换，且更换后 e-cam 无法在变更闭环内验证与回滚。
- 延迟成本：每张三云证书到期都产生一次人工操作 + 无验证窗口/无回滚保障；与已打通的 aliyun/tencent 形成能力落差。

## Proposed Solution

为华为云、AWS、Azure 各实现与 aliyun/tencent 相同的五方法 `CloudDeployer` 端口（UploadCert/BindResource/ListReferences/GetCert/CleanupOrphan），经既有 `CloudAPIChannel` 两段式编排（UploadCert→BindResource，失败 `CleanupOrphan` 补偿）接入变更执行；从 `discoveryOnlyClouds` 移除三云，使其引用进入变更清单可执行项，复用全部既有能力：分批灰度、验证窗口（TLS 探测按域名云无关）、回滚、孤儿清理、云证书映射。

- **华为云**：证书库 = SCM（SSL 证书管理），产品 CDN/WAF/ALB/NLB。
- **AWS**：证书库 = ACM（ImportCertificate），产品 CDN(CloudFront)/ALB/NLB；CloudFront 证书须 us-east-1 地域约束。
- **Azure**：证书库 = Key Vault（证书上传 + 按引用绑定），产品 CDN(Front Door)/ALB(Application Gateway)。

端用户体验：三云引用的证书替换与 aliyun/tencent 完全同构——生成变更清单 → 确认 → 分批执行 → 验证窗口确认 → 旧证书清理，全程无需登录云控制台。

### Innovation Highlights

方案是对既有 `CloudDeployer` 端口 + 两段式编排的**直接沿用**（非新架构）：多云证书更新的第一性原理是「上传到云证书库 → 绑定目标资源 → 验证 → 清理」，各云差异只在 API 形态与证书 ID 空间（SCM ID / ACM ARN / KV 引用），由每云适配层归一。创新点是**验证与回滚闭环的云无关性**——验证窗口按域名 TLS 拨测、回滚按旧云证书 ID 反绑，三云无需新验证/回滚机制。

## Requirements Analysis

### Key Scenarios

- **Happy path**：三云 CDN/ALB/NLB/WAF 引用进入变更清单 → 执行两段式 → 云证书 ID 写入映射 → 验证窗口 TLS 确认线上指纹 = 新证书 → 旧证书孤儿清理。
- **边界**：CloudFront 证书须在 us-east-1 ACM（跨地域）；AWS NLB 监听证书与 ALB 绑定 API 不同；Azure App Gateway 经 KV 证书名称引用而非直接上传 ID；华为 SCM 证书 ID 形态。
- **失败模式**：绑定失败 → `CleanupOrphan` 补偿 + 映射 `active→orphan` 入清理队列（复用现有链路）；云证书 ID 形态归一失败（如 ACM ARN 解析）→ 显式失败不猜测。

### Non-Functional Requirements

- **安全**：私钥明文仅内存传递、用后 Zeroize（对齐既有 Hard Rule）；云端错误细节不进响应仅日志。
- **一致性**：三云部署器与 aliyun/tencent 同一端口语义，失败状态/回滚/清理走同一状态机。
- **可维护性**：每云部署器独立文件 + 独立 fake 测试（复用 certtest 假仓储）。

### Constraints & Dependencies

- 三云 SDK：`huaweicloud-sdk-go-v3`（已在 go.mod v0.1.213）、AWS SDK v2、Azure SDK（需新增依赖）。<!-- 注:实现改为 net/http 直调 REST（internal/shared/cloudx/azure/cert.go 头注"真实 REST 写客户端，无 Azure SDK 依赖"），未新增 Azure SDK 依赖；AWS SDK v2 已入 go.mod（service/acm、service/cloudfront），见文末 Drift Verification -->
- 三云账号凭证复用既有 `CloudAccount` 体系（AK/SK / 凭据）。
- CloudFront 证书地域硬约束 us-east-1；Azure KV 需预先存在 Key Vault 实例。
- 活体验证需真实三云账号（验收阶段提供）。

## Alternatives & Industry Benchmarking

### Industry Solutions

- 云厂商证书生命周期管理（阿里云数字证书/华为云 SCM/AWS Certificate Manager/Azure）都是「上传证书库 + 资源绑定」模型，与 e-cam 两段式同构。
- 多云证书编排工具（如 cert-manager + ExternalDNS、Netflix Lemur）的核心理念是「统一编排 + 各云插件适配」，e-cam 的 CloudDeployer 端口即此模式的轻量实现。

### Comparison Table

| Approach | Source | Pros | Cons | Verdict |
|----------|--------|------|------|---------|
| Do nothing（三云维持人工更换） | — | 零开发 | 每张三云证书到期一次人工操作，无验证/回滚闭环 | Rejected：能力落差持续，与已打通的 aliyun/tencent 不对称 |
| 全部直绑不区分托管 | 简化实现 | 少检测逻辑 | K8s 控制器调谐回滚，替换看似成功实则无效 | Rejected：阿里云 AlbConfig 已验证该风险，三云同理 |
| 扩展 K8s 托管检测到三云 | 控制器标签 | 托管资源自动跳过 | 各云控制器标签约定需逐一验证，工作量大且不确定 | Rejected：本期沿用现有模型（托管资源引导走控制器），增量再做 |
| **Chosen approach：三云五方法部署器接入既有编排** | 既有 CloudDeployer 端口 | 复用全部闭环（分批/验证/回滚/清理/映射），与 aliyun/tencent 同构 | 三云 × 多产品 API 适配工作量 | **Selected：第一性原理下最简路径——上传/绑定/验证/清理语义云无关，只需适配 API 形态** |

## Feasibility Assessment

### Technical Feasibility

- 端口与编排已存在且 aliyun/tencent 验证过；三云 SDK 均提供证书上传与资源绑定 API（华为 SCM/CDN/ELB、AWS ACM/CloudFront/ELB、Azure KV/Front Door/App Gateway），技术上可行。
- 云证书 ID 形态差异（SCM ID/ACM ARN/KV 引用）由每云适配层归一，映射表字段已有承载。

### Resource & Timeline

- 复用既有部署器实现模式（参照 aliyun_deployer.go/tencent_deployer.go），团队能力匹配。
- 工作量主体为三云 × 多产品的 SDK 适配与 fake 测试，按云独立可分阶段交付。

### Dependency Readiness

- 华为 SDK 已在 go.mod；AWS/Azure SDK 需新增（标准依赖，无阻塞）。
- 三云 API 稳定性由云厂商保证；CloudFront us-east-1 与 Azure KV 存在前置资源约束（风险表中列）。

## Assumptions Challenged

| Assumption | Challenge Tool | Finding |
|------------|---------------|---------|
| 三云都必须补齐才值得做 | Need Gate / Occam's Razor | **Refined**：用户确认全产品一次打通（跨域替换场景普遍）；风险通过按云隔离测试与分云交付缓解 |
| 托管资源处理必须扩展 | Assumption Flip | **Overturned**：本期沿用现有模型（托管资源引导走控制器），三云托管检测增量再做 |
| 验证闭环对三云需要新机制 | Stress Test | **Confirmed**：验证按域名 TLS 拨测（ProbeDomains），云无关，直接复用 |
| 云证书 ID 空间跨云可混用 | Assumption Flip | **Confirmed**：三云 ID 空间互斥（SCM ID/ARN/KV 引用），映射唯一性天然成立（无需 aliyun 优先式消歧） |

## Scope

### In Scope

- 华为云部署器（CloudDeployer 五方法）：证书库 SCM，产品 CDN/WAF/ALB/NLB。
- AWS 部署器：证书库 ACM（ImportCertificate），产品 CloudFront/ALB/NLB；CloudFront 证书 us-east-1 地域固定。
- Azure 部署器：证书库 Key Vault，产品 CDN(Front Door)/ALB(Application Gateway)。
- 模块装配：三云 `RegisterDeployer` + `discoveryOnlyClouds` 移除三云 + 发现适配的 `GetCert` 对接。
- 云证书 ID 形态适配：SCM ID / ACM ARN / KV 证书引用，写入 `CloudCertMapping`。
- 测试：三云部署器单元测试（fake 云 SDK，复用 certtest 假仓储）；清单生成三云引用不再 `ERR_DISCOVERY_ONLY` 回归测试。

### Out of Scope

- 三云 K8s 控制器托管资源检测（沿用现有模型，托管资源引导走控制器/CRD 更新）。
- 证书自动签发/续期（只做上传替换）。
- 活体验证（需真实三云账号，验收阶段逐云执行）。
- 多云证书 ID 解析的云内账户级消歧（三云 ID 空间互斥，本期不需要）。

## Key Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| 三云 API 形态/限流差异大（8 个 cloud×product 组合逐一适配） | H | H | 按云独立文件与 fake 测试隔离，逐云交付；参照 aliyun/tencent 既有适配模式 |
| CloudFront 证书必须 us-east-1 ACM（跨地域硬约束） | H | M | UploadCert 固定 us-east-1 地域（参照 CAS 地域固定模式），文档化 |
| AWS NLB / Azure App Gateway 绑定机制特殊（NLB 监听证书 API、AGW 经 KV 引用） | M | M | 按产品分支，复用 aliyun normalize 模式（{certId}-{region}） |
| 无真实三云账号活体验证 | H | M | 单元测试全覆盖 + 提供验证脚本；账号就绪后逐云活体验证（验收标准） |
| Azure/华为证书 ID 与绑定引用形态差异导致映射回滚失败 | M | H | GetCert 回滚校验按云实现；幂等对齐既有 CleanupOrphan 语义 |

## Success Criteria

- [x] 三云产品引用在变更清单中为可执行项（`AutoChangeable=true`），不再 `ERR_DISCOVERY_ONLY`。
- [x] 三云部署器五方法对 9 个 cloud×product 组合（华为 4 + AWS 3 + Azure 2）均有单元测试覆盖（fake 云 SDK），deployer 包测试全绿。
- [x] 三云 `UploadCert` 生成的云证书 ID 正确写入 `CloudCertMapping`（形态按云：SCM ID / ACM ARN / KV 引用）。
- [x] 变更执行失败路径复用既有补偿：`CleanupOrphan` 幂等 + 映射 `active→orphan` 入清理队列。
- [x] 验证窗口对三云目标域名复用 `ProbeDomains` 判定线上指纹 = 新证书（云无关闭环，无需新验证机制）。
- [x] 回滚路径：`GetCert` 校验旧云证书有效后 `BindResource` 恢复旧 ID（每云单元测试覆盖）。

## Drift Verification (2026-09-16, T-quick-doc-drift)

对照实际实现（commits）与测试结果逐项核对 Success Criteria，结论：**6 项 SC 全部满足；1 处约束行文本级漂移已标注（见上方 Constraints 注），其余全项一致**。

| 核对项 | 实测/实况 | 结论 |
|---|---|---|
| SC1 清单可执行 | svc `dfc4624`：`changelist_generator.go` `assessChangeable` 云通道恒可执行（338 行注释：discoveryOnlyClouds 随三云落地移除，ERR_DISCOVERY_ONLY 分区不再产生）；测试 `TestGenerateChangeList_ThreeCloudReferencesExecutable` / `TestGenerateChangeList_AllNineCombosExecutable` PASS | 一致 |
| SC2 五方法 9 组合覆盖 + deployer 包全绿 | `internal/cert/deployer/{huawei,aws,azure}_deployer_test.go` 在库；`go test ./internal/cert/deployer/...` ok（1.811s，本次复跑确认）；journey 级 matrix 18 用例（三云 9 组合逐项）全 PASS | 一致 |
| SC3 云证书 ID 写映射 | `TestExecuteMappingWrittenActivePerCloudForm` / `TestAllCombos_MappingConsistentAfterExecution` PASS；Azure KV 引用形态（KV secret ID 含版本）见 `internal/shared/cloudx/azure/cert.go` 头注口径 | 一致 |
| SC4 失败补偿 | `TestCompensation_DoubleInvocationIdempotent` / `TestCompensation_OrphanTransitionEnqueuesCleanup` / `TestCleanupQueue_*` 全 PASS | 一致 |
| SC5 验证窗口复用 | `verify_window_service.go:391` 复用 `ProbeDomains`（按域名云无关）；`TestVerifyWindow_ConsecutiveProbesConfirmCompletion` / `TestVerifyWindow_ProbeMismatchNotPassedAndExpiryFinalization` PASS | 一致 |
| SC6 回滚 GetCert 校验 | `rollback_service.go:21` GetCert 三判定（云侧已删除/已过期/指纹被替换）；`TestRollbackPrecheck_PassesForValidOldCert` / `TestRebind_RestoresOldCloudCertReference` / `TestAwsCloudFront_RebindIdempotentTerminalState` PASS | 一致 |
| Constraints：三云 SDK | 华为 `huaweicloud-sdk-go-v3` v0.1.213（原文一致）；AWS SDK v2 已入 go.mod（`service/acm` v1.38.0、`service/cloudfront` v1.60.2）；**Azure 未新增 SDK**——实现为 net/http 直调 REST（证书库 KV、2024-02-01 api-version），适配层单点归一，属原文「需新增依赖」的文本级漂移 | **漂移，已标注** |
| 装配（module.go） | 5 个 `RegisterDeployer`（aliyun/tencent + 新增 huawei 4 产品、aws 3 产品、azure 2 产品），产品集与 proposal 完全对应 | 一致 |
| CloudFront us-east-1 | `TestAwsCloudFront_UploadPinnedToUseast1` / `TestAwsCloudFront_DefaultRegionIndependent` PASS | 一致 |
| NFR 安全 | `deployer/channel.go` `Credential.Zeroize`（幂等、nil 安全、禁序列化）；云错误细节经 `wrapCertCloudErr` 归一为哨兵错误不进响应；三云部署器独立文件 + 独立 fake 测试（`tests/multicloudtest` 活体服务桩） | 一致 |
| Out of Scope 未混入 | K8s 托管检测未扩展（仍 probe 通道判定）；无自动签发/续期；活体验证未做（fake SDK，真实账号验收另做）；无云内账户级消歧（`TestCrossCloudMixingRejected` 反向验证 ID 空间互斥） | 一致 |
| 测试全绿 | 67/67 活体 API 功能测试（bind-failure-compensation 14 / multicloud-cert-replacement 21 / rollback-restore-old-cert 14 / three-cloud-product-matrix 18，tests/latest.md，commit `6c33529`） | 一致 |

其余发现（非 spec 文件，登记待后续任务处理）：`internal/cert/service/rollback_service.go:60` 注释「discovery-only 三云无成功项场景天然不触达」为本特性落地前（commit `4df46cf`）的旧口径——三云现已注册部署器，其 success 项会触达 `InspectCloudCert` 回滚校验，该注释已过时；留待 T-validate-code（代码注释归属代码任务）修正，不在本 doc 任务内改动 .go 文件。

本次为 quick 模式特性，docs/business-rules/ 与 docs/conventions/ 项目级 spec 目录不存在，项目级 spec 无漂移对象。

## Next Steps

- Proceed to `/write-prd` to formalize requirements

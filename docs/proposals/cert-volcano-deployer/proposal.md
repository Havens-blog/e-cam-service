---
created: "2026-09-19"
author: "Haven"
status: Approved
intent: "new-feature"
---

# Proposal: 火山云证书替换上线（四产品部署器 + 引用扫描）

## Problem

火山证书已可导入台账（`cert-volcano-import-sync` 完成），但**替换仍限五云**——火山 CDN/WAF/ALB/NLB 引用的证书到期只能人工登录火山控制台更换，无 e-cam 变更清单闭环（分批灰度、验证窗口、回滚、孤儿清理）。证书主流采购已在火山，替换能力落差成为高频运维痛点。

### Evidence

- 代码事实：`internal/cert/module.go` `RegisterDeployer` 仅 5 云（aliyun/tencent/huawei/aws/azure）；`reference_scan_service.go` 扫描适配器 5 云口径；`cert-volcano-import-sync` proposal 明确「火山替换」Out of Scope。
- SDK 事实：`volcengine-go-sdk v1.2.9`（已在 go.mod，cloudx/volcano 依赖）三产品证书 API 齐备——**CDN**（`AddCdnCertificate`/`BatchDeployCert`/`ListCdnCertInfo`/`DescribeCertConfig`）、**WAF**（`CreateDomain`/`ListWafServiceCertificate`/`QueryCertificateIfReplace`）、**ALB**（`CreateListener`/`DescribeAllCertificates`/`DeleteCertificate`）、**CLB/NLB**（`CreateNlbListener`/`DeleteCertificate`）；`certificateservice`（`ImportCertificate`/`GetInstance`/`DeleteInstance`）作上传/回滚/清理基础。
- 已有管线全复用：`CloudDeployer` 五方法端口（`deployer/channel.go:302`）、两段式编排（UploadCert→BindResource→失败 `CleanupOrphan` 补偿）、验证窗口（`ProbeDomains` 域名云无关）、回滚（`GetCert` 校验+反绑）、mapping `uk_fp_cloud_account`、`resolveNewCertID` aliyun-preference。

### Urgency

火山为证书主流采购地，到期替换是高频运维动作；当前每次人工 + 无验证窗口/回滚保障。延后成本：每张火山证书到期一次人工操作；与已打通的五云形成能力落差（同 `cert-multicloud-deployers` 当时对三云的判断，本次落火山）。

## Proposed Solution

1. **火山部署器**（`internal/cert/deployer/volcano_deployer.go`）实现 `CloudDeployer` 五方法 × 4 产品：
   - `UploadCert`：证书束上传至火山证书库，返回云证书 ID。**证书库形态按产品归一**（火山各产品证书库独立：CDN `AddCdnCertificate` / WAF 服务证书 / ALB-NLB 监听证书 / certificateservice `ImportCertificate`），云证书 ID 形态 `{product}:{id}` 前缀归一（对齐 huawei SCM / AWS ACM ARN 归一模式）。
   - `BindResource`：CDN=`BatchDeployCert`(加速域名)；WAF=域名证书替换；ALB/NLB=监听证书更新（对齐既有 alb/nlb 监听复合 ID + served domains 展开先例）。
   - `ListReferences`：四产品资源 → `CertReference` 指纹解析（对齐 3.5 口径：映射反查 → GetCert 要素 → 确定性占位指纹）。
   - `GetCert`：证书在库状态校验（回滚目标有效性判定）。
   - `CleanupOrphan`：`DeleteInstance` 幂等清理。
2. **火山引用扫描适配器**：接入 `reference_scan_service.go`（对齐 5 云 `NewXxxScanAdapter` 形态），火山引用进入变更清单可执行项。
3. **装配**：`module.go` `RegisterDeployer` 第 6 云（volcano×4 产品）+ 扫描适配器列表加入火山。
4. **全复用**：分批灰度（≤50%）、验证窗口（TLS 拨测按域名云无关）、回滚、孤儿清理、mapping —— 零新机制。

### Innovation Highlights

对既有 `CloudDeployer` 端口 + 两段式编排的**直接沿用**：替换第一性原理（上传到云证书库 → 绑定目标资源 → 验证 → 清理）火山与五云一致，差异只在证书库形态与绑定 API。关键判断：**各产品独立证书库不阻塞两段式**——UploadCert 按产品上传即得该库 ID，BindResource 引用之，云证书 ID 形态按产品归一。验证/回滚闭环云无关，直接复用。

## Requirements Analysis

### Key Scenarios

- **Happy path**：火山 CDN 引用到期 → 变更清单发现（引用扫描）→ 确认 → 两段式执行（UploadCert→BindResource）→ 验证窗口 TLS 确认线上=新指纹 → 旧证书孤儿清理。
- **产品分支**：CDN/WAF/ALB/NLB 各自绑定 API；资源 ID 形态按产品归一。
- **失败模式**：绑定失败 → `CleanupOrphan` 补偿 + 映射 `active→orphan` 入清理队列（复用既有状态机）。
- **回滚**：`GetCert` 校验旧云证书 ID 有效 → `BindResource` 恢复旧 ID（每产品覆盖）。
- **私钥缺失**：fingerprint_only 台账证书不可上传 → 变更清单生成评估拦截（既有通用语义，与五云一致）。

### Non-Functional Requirements

- **安全**：私钥明文仅内存传递、用后 Zeroize（对齐既有 Hard Rule）；云侧错误细节不进响应仅日志。
- **一致性**：四产品与五云同一端口语义，失败/回滚/清理走同一状态机。
- **只读纪律**：引用扫描只读；写操作仅存在于执行面（deployer）。
- **可维护性**：火山部署器独立文件 + 独立 fake 测试（复用 certtest / multicloudtest 桩）。

### Constraints & Dependencies

- `volcengine-go-sdk v1.2.9` 已在 go.mod；火山账号 provider `volcano`/`volcengine` 已登记、`accountScanSource.ActiveByCloud` 已支持枚举。
- 依赖 `cert-volcano-import-sync` 交付的火山证书库适配器（`cloudx/volcano/cert.go`）作 GetCert/上传基础。
- 依赖 `644b067`（checkChain 系统信任库回退）——火山链缺根可正常处理。

## Alternatives & Industry Benchmarking

### Industry Solutions

云厂商证书生命周期（阿里 CAS/火山 certificateservice/ACM）都是「证书库 + 资源绑定」；e-cam `CloudDeployer` 端口是 cert-manager/Lemur 编排模式的轻量实现。本 feature 是既有端口的第 6 云接入，无新范式。

### Comparison Table

| Approach | Source | Pros | Cons | Verdict |
|----------|--------|------|------|---------|
| Do nothing（火山控制台人工更换） | — | 零开发 | 每张火山证书到期一次人工 + 无验证/回滚闭环 | Rejected：高频痛点，能力落差持续 |
| 仅 CDN 部署器 | 简化实现 | 覆盖最常见挂载面 | WAF/ALB/NLB 仍人工，闭环不完整 | Rejected：用户已确认四产品全量 |
| **Chosen：四产品部署器 + 引用扫描 + 装配** | 既有 CloudDeployer 端口 + 5 云先例 | 全复用（分批/验证/回滚/清理/映射），与 cert-multicloud-deployers 同构 | 火山 4 产品证书库/绑定 API 适配工作量 | **Selected：替换语义云无关，只需适配证书库形态与绑定 API；先例证明单云多产品 quick 可容** |

## Feasibility Assessment

### Technical Feasibility

SDK 四产品证书 API 已逐一探明齐备；端口与编排已生产验证（五云）；火山证书库适配器已就绪。缺口仅火山部署器 + 扫描适配器 + 装配 + fake 测试，无 showstopper。

### Resource & Timeline

参照 `huawei_deployer.go`（4 产品先例）与 `deployer_common.go` 共享助手，团队能力匹配；quick 模式单 feature 可容（对齐 cert-multicloud-deployers 单云 4 产品体量）。

### Dependency Readiness

`volcengine-go-sdk v1.2.9` 在 go.mod；四产品证书 API 由云厂商保证；无新增依赖。

## Assumptions Challenged

| Assumption | Challenge Tool | Finding |
|------------|---------------|---------|
| 火山必须统一证书库才能做两段式 | Assumption Flip | **Overturned**：各产品独立证书库也可两段式——UploadCert 按产品上传即得该库 ID，BindResource 引用之；云证书 ID 形态按产品前缀归一 |
| `resolveNewCertID` 需为火山扩展跨云消歧 | Provable from codebase | **Confirmed（机制面）**：火山 instanceId 与五云 ID 空间互斥（对齐三云 ID 空间互斥测试口径），aliyun-preference 逻辑无需改动 |
| K8s CRD `patch_crd` 需支持火山 | Need Gate | **Overturned**：`patch_crd` 走既有 K8s 通道（集群内 CRD 证书字段），与云无关；火山不引入新机制 |
| 私钥缺失会阻塞本 feature | Occam's Razor | **Confirmed（既有语义）**：fingerprint_only 不可上传由清单生成评估拦截，与五云一致，非火山特有 |

## Scope

### In Scope

- 火山部署器 `CloudDeployer` 五方法 × 4 产品（CDN/WAF/ALB/NLB）：`internal/cert/deployer/volcano_deployer.go` + fake SDK 单测。
- 火山引用扫描适配器（四产品资源→CertReference）：`internal/cert/service/` 对齐 5 云形态 + 单测。
- `module.go`：`RegisterDeployer`（volcano×4）+ 扫描适配器列表装配（第 6 云）。
- 变更清单回归：火山引用 `AutoChangeable=true`（不再 `ERR_DISCOVERY_ONLY`/skipped）。
- 两段式编排/验证窗口/回滚/孤儿清理的火山接通验证（复用，无新机制）。
- 共享助手复用：`deployer_common.go`（withRetry/generateUploadName/account/fingerprint）。

### Out of Scope

- K8s CRD `patch_crd` 扩展（走既有 K8s 通道）。
- 火山自动签发/续期（只做上传替换）。
- 活体验证（无真实火山账号，fake SDK + multicloudtest 桩；验收阶段逐产品）。
- 跨产品证书库互拷优化（火山产品库独立为固有形态，不做统一）。

## Key Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| 火山 4 产品证书库/绑定 API 形态差异大（4 库归一） | H | M | 按产品分支适配 + 独立 fake 测试；参照 huawei/aws/azure 归一模式（SCM ID/ARN/KV 引用先例） |
| CDN `BatchDeployCert` 域绑定粒度（单/多域名）与 ListReferences 对齐 | M | M | 引用扫描按域名粒度（resourceId=域名）；BindResource 按加速域名绑定 |
| ALB/NLB 监听证书更新 API 幂等/替换语义 | M | M | 对齐既有 alb/nlb 监听复合 ID + served domains 展开先例；幂等重跑测试 |
| 无真实火山账号活体验证 | H | M | fake SDK 全覆盖 + 提供验证脚本；账号就绪后逐产品活体（验收标准） |
| UploadCert 私钥缺失（fingerprint_only） | M | L | 沿用清单生成评估拦截语义（与五云一致），非新风险面 |

## Success Criteria

- [ ] 火山部署器五方法 × 4 产品 fake SDK 单测覆盖（10+ 用例/组合），deployer 包测试全绿。
- [ ] 引用扫描适配器：火山 CDN/WAF/ALB/NLB 资源 → `CertReference` 指纹解析正确（对齐 3.5 口径；占位指纹 `certscan-unresolved:` 语义一致）。
- [ ] `module.go` 第 6 云装配：`RegisterDeployer`（volcano×4）+ 扫描适配器；清单生成测试火山引用 `AutoChangeable=true`（回归，不再 skipped）。
- [ ] 两段式执行：UploadCert→BindResource 后云证书映射写入 active，云证书 ID 形态 `{product}:{id}` 归一断言。
- [ ] 失败补偿：绑定失败 → `CleanupOrphan` 幂等（双调用同结果）+ 映射 `active→orphan` 入清理队列（复用既有测试断言）。
- [ ] 验证窗口：火山目标域名经 `ProbeDomains` 判定线上指纹 = 新证书（云无关复用；`TestVerifyWindow_*` 先例）。
- [ ] 回滚：`GetCert` 校验旧 ID 有效 → `BindResource` 恢复旧 ID（每产品覆盖用例）。
- [ ] 回归：现有五云部署/清单/验证测试全绿（新增第 6 云不破坏既有行为）。

## Next Steps

- Proceed to `/quick-tasks`（quick 模式）生成任务并执行。
- 活体验证：火山账号就绪后逐产品（CDN/WAF/ALB/NLB）执行替换验收。

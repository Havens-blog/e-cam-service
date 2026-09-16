---
id: "3"
title: "Azure 部署器：Key Vault 证书库 + CDN(Front Door)/ALB(App Gateway) 两产品绑定"
priority: "P0"
estimated_time: "3h"
complexity: "high"
dependencies: []
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 3: Azure 部署器：Key Vault 证书库 + CDN(Front Door)/ALB(App Gateway) 两产品绑定

## Description

实现 Azure 五方法 `CloudDeployer`：证书库 = Key Vault（证书导入），产品 = CDN(Front Door)/ALB(Application Gateway)。与 aliyun/tencent 部署器同构，两段式编排由既有 CloudAPIChannel 承担。Azure 特殊点：App Gateway 经 Key Vault 证书**名称/引用**绑定而非直接上传 ID；Front Door 经自定义域名 HTTPS 配置引用 KV 证书。

## Reference Files
- `docs/proposals/cert-multicloud-deployers/proposal.md` — Proposed Solution / In Scope / Key Risks
- `internal/cert/deployer/aliyun_deployer.go` — 五方法部署器实现模式
- `internal/cert/deployer/channel.go` — CloudDeployer 端口定义
- `internal/shared/cloudx/azure/cert_discovery_adapter.go` — 既有发现适配（ListReferences/GetCert 复用）
- `internal/cert/module.go` — 云适配装配点（NewAzureScanAdapter 消费）

## Acceptance Criteria

- [ ] `UploadCert`：证书导入 Key Vault（PFX/PEM 形态，含私钥的 KV 证书导入 API），返回 KV 证书引用（名称/版本）；私钥明文仅内存、用后 Zeroize
- [ ] `BindResource`：按产品路由绑定——CDN(Front Door) 自定义域名 HTTPS 配置、ALB(Application Gateway) 监听器 HTTP2/SSL 证书引用（经 KV 证书名称引用，非直接上传 ID）
- [ ] `GetCert`：KV 证书在库状态（引用存在性 + SHA256 指纹对齐口径）+ 回滚目标有效性校验
- [ ] `CleanupOrphan`：KV 证书删除/禁用（幂等；KV 软删除语义处理）
- [ ] `ListReferences`：复用发现适配引用形态 + 指纹解析（映射反查 → GetCert fallback → 确定性占位指纹）
- [ ] 单元测试：fake Key Vault/Front Door/App Gateway SDK 覆盖五方法 × 两产品（含 KV 引用归一化、限流退避、绑定失败补偿路径）

## Hard Rules

- 仅修改：`internal/shared/cloudx/azure/`（新增完整 CertAdapter 或扩展既有发现适配）、`internal/cert/deployer/`（新增 azure_deployer.go + 测试）、`internal/cert/module.go`（装配，Task 4 收敛）；不触碰其他云
- 私钥明文仅内存传递、用后 Zeroize，禁入日志/错误文案
- Azure 证书引用为 KV 名称（非直接证书 ID），绑定与回滚统一走 KV 引用形态（归一化在适配层）
- 限流退避有界（复用既有 RetryPolicy；Azure 请求计数限流语义区分）

## Implementation Notes

- Azure SDK 需新增 go.mod 依赖（标准依赖，无阻塞）；KV 证书导入 API 形态（含私钥的 PFX base64）需按 SDK 确认
- App Gateway 绑定：监听器 SSL 证书引用 KV 证书 URL（`https://{vault}.vault.azure.net/secrets/{name}/{version}`）——UploadCert 返回的「云证书 ID」即此 URL 形态，映射表直接承载
- Front Door 绑定：自定义域名 HTTPS 配置的 Key Vault 证书引用，与 AGW 同引用形态
- 既有 `cert_discovery_adapter.go`（discovery-only）保留或由完整 CertAdapter 取代；扫描适配与部署器共享同一 CertAdapter 实例（aliyun 模式），装配收敛在 Task 4

---
id: "2"
title: "AWS 部署器：ACM 证书库 + CloudFront/ALB/NLB 三产品绑定"
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

# 2: AWS 部署器：ACM 证书库 + CloudFront/ALB/NLB 三产品绑定

## Description

实现 AWS 五方法 `CloudDeployer`：证书库 = ACM（ImportCertificate），产品 = CloudFront/ALB/NLB。CloudFront 证书须 us-east-1 ACM（AWS 硬约束）；ALB/NLB 经监听证书（ListenerCertificate/Listener 的 Certificate 引用）绑定，ALB 与 NLB 绑定 API 不同。与 aliyun/tencent 部署器同构，两段式编排由既有 CloudAPIChannel 承担。

## Reference Files
- `docs/proposals/cert-multicloud-deployers/proposal.md` — Proposed Solution / In Scope / Key Risks
- `internal/cert/deployer/aliyun_deployer.go` — 五方法部署器实现模式
- `internal/cert/deployer/channel.go` — CloudDeployer 端口定义
- `internal/shared/cloudx/aws/cert_discovery_adapter.go` — 既有发现适配（ListReferences/GetCert 复用）
- `internal/cert/module.go` — 云适配装配点（NewAwsScanAdapter 消费）

## Acceptance Criteria

- [ ] `UploadCert`：`ImportCertificate` 上传证书束至 ACM 返回 ACM ARN；**CloudFront 证书固定 us-east-1 地域**（其余产品可用账号主地域）；私钥明文仅内存、用后 Zeroize
- [ ] `BindResource`：按产品路由绑定——CloudFront 分发（ViewerCertificate/Aliases）、ALB 监听证书（`AddListenerCertificates` 形态）、NLB 监听证书（`AddListenerCertificates` 或 UpdateListenerAttribute，按实际 API 分支）
- [ ] `GetCert`：ACM 证书在库状态（ARN 存在性 + SHA256 指纹对齐口径）+ 回滚目标有效性校验
- [ ] `CleanupOrphan`：ACM 导入证书删除（`DeleteCertificate`，已删除幂等成功；AWS 托管证书不可删须显式识别）
- [ ] `ListReferences`：复用发现适配引用形态 + 指纹解析（映射反查 → GetCert fallback → 确定性占位指纹）
- [ ] 单元测试：fake ACM/CloudFront/ELBv2 SDK 覆盖五方法 × 三产品（含 CloudFront us-east-1 地域断言、限流退避、绑定失败补偿路径）

## Hard Rules

- 仅修改：`internal/shared/cloudx/aws/`（新增完整 CertAdapter 或扩展既有发现适配）、`internal/cert/deployer/`（新增 aws_deployer.go + 测试）、`internal/cert/module.go`（装配，Task 4 收敛）；不触碰其他云
- 私钥明文仅内存传递、用后 Zeroize，禁入日志/错误文案
- CloudFront 证书上传地域恒 us-east-1（硬约束，文档化 + 单测锁定）
- 限流退避有界（复用既有 RetryPolicy；AWS SDK 自带重试与限流语义须区分对待）

## Implementation Notes

- AWS SDK v2 需新增 go.mod 依赖（标准依赖，无阻塞）；`ImportCertificate` 私钥明文经内存传参后 Zeroize
- CloudFront 的 ViewerCertificate 配置与 ACM 证书的 us-east-1 绑定是 AWS 平台约束——UploadCert 固定地域、BindResource 按分发 ID 定位
- AWS ALB/NLB 监听证书绑定 API 不同（ALB `AddListenerCertificates`，NLB 实际形态按 SDK 确认），按产品分支复用 aliyun normalize 模式
- 既有 `cert_discovery_adapter.go`（discovery-only）保留或由完整 CertAdapter 取代；扫描适配与部署器共享同一 CertAdapter 实例（aliyun 模式），装配收敛在 Task 4

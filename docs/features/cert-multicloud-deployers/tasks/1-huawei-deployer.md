---
id: "1"
title: "华为云部署器：SCM 证书库 + CDN/WAF/ALB/NLB 四产品绑定"
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

# 1: 华为云部署器：SCM 证书库 + CDN/WAF/ALB/NLB 四产品绑定

## Description

实现华为云五方法 `CloudDeployer`（UploadCert/BindResource/ListReferences/GetCert/CleanupOrphan）：证书库 = SCM（SSL 证书管理），产品 = CDN/WAF/ALB/NLB。与 aliyun/tencent 部署器同构——两段式编排（UploadCert→BindResource，失败 CleanupOrphan 补偿）由既有 CloudAPIChannel 承担，本层只做 SDK 适配 + 限流退避 + 上传名生成 + ID 归一化。

## Reference Files
- `docs/proposals/cert-multicloud-deployers/proposal.md` — Proposed Solution / In Scope / Key Risks
- `internal/cert/deployer/aliyun_deployer.go` — 五方法部署器实现模式（限流退避/上传名/ID 归一化/补偿）
- `internal/cert/deployer/channel.go` — CloudDeployer 端口定义
- `internal/shared/cloudx/huawei/cert_discovery_adapter.go` — 既有发现适配（ListReferences/GetCert 复用指纹解析路径）
- `internal/cert/module.go` — 云适配装配点（NewHuaweiScanAdapter 消费）

## Acceptance Criteria

- [ ] `UploadCert`：上传证书束至华为云 SCM 返回 SCM 证书 ID；上传名唯一生成（复用 `ecam-{指纹前8}-{unix秒}-{随机}` 模式，≤63 字符）；私钥明文仅内存、用后 Zeroize
- [ ] `BindResource`：按产品路由绑定——CDN 加速域名、WAF 域名、ELB（ALB/NLB 监听）证书引用；ELB 监听证书 ID 形态归一化（参照 aliyun `{certId}-{region}` 模式）
- [ ] `GetCert`：SCM 证书在库状态（存在性 + SHA256 指纹对齐口径）+ 回滚目标有效性校验
- [ ] `CleanupOrphan`：SCM 孤儿证书删除（已删除幂等成功）
- [ ] `ListReferences`：复用发现适配引用形态 + 指纹解析（映射反查 → GetCert fallback → 确定性占位指纹）
- [ ] 单元测试：fake SCM/CDN/WAF/ELB SDK 覆盖五方法 × 四产品（含限流退避分支、上传名冲突换名、绑定失败补偿路径）

## Hard Rules

- 仅修改：`internal/shared/cloudx/huawei/`（新增完整 CertAdapter 或扩展既有发现适配）、`internal/cert/deployer/`（新增 huawei_deployer.go + 测试）、`internal/cert/module.go`（装配，Task 4 收敛）；不触碰其他云
- 私钥明文仅内存传递、用后 Zeroize，禁入日志/错误文案
- 云端错误细节不进响应（静态失败文案，与 aliyun/tencent 同口径）
- 限流退避有界（MaxAttempts/MaxTotalWait 双闸，复用既有 RetryPolicy）

## Implementation Notes

- 参照 `aliyun_deployer.go` 的 `withRetry`/`generateUploadName`/`normalizeAliyunListenerCertID` 模式逐项移植；华为云 SDK 已在 go.mod（huaweicloud-sdk-go-v3 v0.1.213）
- 华为云 CDN/WAF/ELB 证书绑定 API 形态需逐一确认（SCM 上传返回 cert id；CDN/WAF 域名级绑定；ELB 监听级绑定 API 与 aliyun ALB/NLB 差异）
- 既有 `cert_discovery_adapter.go`（discovery-only）保留或由完整 CertAdapter 取代：扫描适配（`NewHuaweiScanAdapter`）与部署器共享同一 CertAdapter 实例（aliyun 模式），装配收敛在 Task 4
- 指纹解析口径与 3.5 一致：映射反查 → GetCert 要素（仅 SHA256 对齐口径）→ 确定性占位指纹

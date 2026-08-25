---
id: "1"
title: "四云 GetCert 通道扩展暴露净化 PEM"
priority: "P0"
estimated_time: "2h"
complexity: "high"
dependencies: []
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 1: 四云 GetCert 通道扩展暴露净化 PEM

## Description

云适配层 GetCert 当前只取指纹即丢弃 PEM。本任务为发现导入提供材料通道：阿里/腾讯/Azure/AWS 四云 GetCert 扩展返回"仅 CERTIFICATE 块的净化序列"（叶在前 fullchain 口径），AWS 补 CertificateChain 拼接与 IAM-hosted 非 ARN ID 降级标记，华为返回不支持标记。这是提案风险表首号风险（AWS fullchain 口径缺口）的落地任务（quick 模式无 tech-design，本任务即首任务）。

## Reference Files
- `docs/proposals/cert-cloud-discovery-import/proposal.md` — In Scope 四云 PEM 通道条目、Non-Functional Requirements 安全条目、Key Risks 首两行、Success Criteria SC-4/SC-9 (ref: In Scope; Non-Functional Requirements; Key Risks; Success Criteria)
- `internal/shared/cloudx/aliyun/cert.go`: GetCert 扩展返回净化 PEM（response.Cert 已含 PEM，当前 parseCertLeafPEM 后丢弃） (ref: Evidence)
- `internal/shared/cloudx/tencent/cert.go`: GetCert 扩展返回净化 PEM（CertificatePublicKey） (ref: Evidence)
- `internal/shared/cloudx/azure/cert_discovery.go`: KeyVault secret 全量值必须走仅 CERTIFICATE 块净化（exportable 密钥策略下含私钥），原始 buffer Zeroize (ref: Non-Functional Requirements)
- `internal/shared/cloudx/aws/cert_discovery.go`: 读取并拼接 output.Certificate + output.CertificateChain（叶在前）；非 ARN（IAM-hosted）ID 显式返回降级标记错误 (ref: Key Risks)

## Acceptance Criteria

- [ ] 阿里/腾讯/Azure GetCert 返回净化 PEM 序列：块级过滤仅保留 CERTIFICATE 块，丢弃 PRIVATE KEY/PKCS#12 等任何非 CERTIFICATE 内容；AWS 按叶在前拼接 Certificate+CertificateChain
- [ ] 净化内容级断言（单测）：四云 fake 返回值不含 "PRIVATE KEY" 字样、含且仅含 CERTIFICATE 块；Azure secret 含私钥 bundle 时私钥被丢弃
- [ ] AWS IAM-hosted（非 ARN 形态）证书 ID 返回显式"不支持"标记错误（可被上层识别为降级，非通用失败）；华为 GetCert 返回不支持 PEM 标记
- [ ] 净化前原始 buffer（尤其 Azure secret 值）用后 Zeroize；错误路径文案为静态文案，不携带云响应片段
- [ ] 四云单测补齐：fake 适配器覆盖 fullchain 组成、Azure 私钥 bundle 净化、AWS CertificateChain 拼接与 IAM-hosted 分支

## Hard Rules

- 扫描只读面约束不变：适配层仅暴露只读能力，禁入任何写通路（UploadCert/BindResource/CleanupOrphan 不受本任务影响）
- PEM 净化必须是构造性保证（块级过滤实现于适配/服务层），不得依赖调用方约定

## Implementation Notes

- AWS GetCert 现状：已消费 output.Certificate 并解析叶 PEM（cert_discovery.go:380-397），缺口是 CertificateChain 从未读取；非 ARN ID 在 :370-372 显式报错——本任务将该错误升级为结构化降级标记
- fullchain 口径与手工导入对齐：leaf + 中间 CA + 自签根（certtest 约定）；台账 CertPEM 是后续换证时部署器 UploadCert 的上传材料源，leaf-only 会在换证执行期才暴露缺陷
- 联调期建议附与 doc-fix-1 平行的各云真实响应手动验证清单（非本任务验收项；单测以 fake 适配器覆盖）

### Test Impact
- Affected test suite(s): internal/shared/cloudx/{aliyun,tencent,azure,aws}/*_test.go
- Expected fixture changes: fake 适配器补 CertificateChain / Azure 私钥 bundle / IAM-hosted 用例
- Risk level: low

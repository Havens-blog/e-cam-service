---
id: "1"
title: "火山部署器证书库层（UploadCert/GetCert/CleanupOrphan）"
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

# 1: 火山部署器证书库层（UploadCert/GetCert/CleanupOrphan）

## Description

火山部署器的第一段（上传）+ 回滚/清理基础：`CloudDeployer` 三方法在火山证书库上的实现与云证书 ID 归一。火山各产品证书库独立——上传按产品落到对应证书库（CDN `AddCdnCertificate` / WAF 服务证书 / ALB-NLB 监听证书 / certificateservice `ImportCertificate`），云证书 ID 归一为 `{product}:{id}`（对齐 huawei SCM / AWS ACM ARN 归一模式）。共享助手（`deployer_common.go` withRetry/generateUploadName/account/fingerprint）复用。

## Reference Files

- `docs/proposals/cert-volcano-deployer/proposal.md` — Proposed Solution / In Scope / Key Risks / Success Criteria
- `internal/cert/deployer/huawei_deployer.go` — 4 产品部署器先例（UploadCert/GetCert/CleanupOrphan 结构）
- `internal/cert/deployer/deployer_common.go` — 共享助手（withRetry/generateUploadName/account/fingerprint）
- `internal/cert/deployer/channel.go` — CloudDeployer 端口与 CloudCertInfo 形态（302-314 行）
- `internal/shared/cloudx/volcano/cert.go` — 火山证书库适配器（cert-volcano-import-sync 交付，GetCert 基础）

## Acceptance Criteria

- [ ] `UploadCert` 按产品分支上传至对应火山证书库（CDN/WAF/ALB/NLB/certificateservice），返回 `{product}:{id}` 归一云证书 ID；PEM 链与私钥传递语义对齐五云（私钥仅内存、用后 Zeroize）
- [ ] `GetCert` 经 certificateservice/产品证书库查询在库状态（存在/过期/指纹），回滚目标有效性判定语义与五云一致
- [ ] `CleanupOrphan` 对已删除证书幂等成功（双调用同结果，对齐 3.1/3.2 口径）
- [ ] 私有方法签名与 `Credential`/`domain.CloudCertInfo` 形态对齐 channel.go 端口（编译期断言）
- [ ] fake SDK 单测覆盖：上传成功/失败、ID 归一断言（含多产品分支）、GetCert 三判定、CleanupOrphan 幂等
- [ ] 只依赖 volcengine-go-sdk v1.2.9 既有包，不新增依赖

## Hard Rules

- 私钥明文仅内存传递、用后 Zeroize（对齐既有 Hard Rule）；私钥不进日志/错误信息/返回结构。
- 仅修改以下文件：`internal/cert/deployer/volcano_deployer.go`、`internal/cert/deployer/volcano_deployer_test.go`（+ fake 支持文件）。

## Implementation Notes

### Key Risks 缓解

- 4 产品证书库形态差异（H/M）：按产品分支适配，独立 fake 测试；参照 huawei SCM/aws ACM ARN/azure KV 引用三先例的归一策略。
- 证书链缺根：`644b067` 已支持系统信任库回退，上传前链校验对齐既有 `checkChain` 语义。

### File Scope

新增：`internal/cert/deployer/volcano_deployer.go`（本任务范围：UploadCert/GetCert/CleanupOrphan + ID 归一 + 证书库分支）、`volcano_deployer_test.go`（fake SDK）。绑定层（BindResource/ListReferences）由任务 2 追加到同一文件或独立文件——本任务先建骨架与共享结构。

### Test Impact

- 新文件 + 新测试；`TestCompensation_*`、`TestCleanupQueue_*` 既有断言若被复用不得破坏。风险 low。
---
created: "2026-08-14"
prd: prd/prd-spec.md
status: Draft
---

# Technical Design: SSL 证书统一托管与更换（证书管理功能域）

> 上游：PRD（916/1000 通过）、UI 设计（972/1000 通过）。
> Override: API handbook enabled by signal "新 API/接口"（证书管理全新接口面，见 design/api-handbook.md）
> Override: Security Review enabled by signal "加密/权限/私钥"（私钥信封加密、EIAM 权限）

## Overview

新增 `internal/cert` 功能域，沿用 e-cam DDD 分层（domain/repository/service/web/module + ioc wire 注入）。复用多云 SDK 适配层（`internal/shared/cloudx`，扩展证书 API）、异步任务框架、`internal/alert` 通道框架、`internal/audit` 审计、EIAM。新增依赖 `k8s.io/client-go`；证书解析与 TLS 探测用 Go 标准库 `crypto/x509` + `crypto/tls`。

核心抽象：`ExecutionChannel` 接口隔离"发现/部署/回滚"与目标资源类型，云 API 与 K8s 两实现首期落地，堡垒机/优维 Agent 接口预留二期。

## Architecture

### Layer Placement

`internal/cert`（与 `internal/cam` 平级新域），通过 `ioc/cert.go` 注入 Wire。前端落在 e-cam-web 新增 `views/modules/cert/` 路由模块。

### Component Diagram

```
[e-cam-web] ──HTTP/Gin──▶ [cert/web (handlers)]
                              │
                       [cert/service]
            ┌─────────────────┼──────────────────────────┐
   [cert/repository→MongoDB]  [cert/deployer]        [scheduler 定时任务]
            │            ┌────────┴────────┐      (scan/probe/inspection)
       [audit/audit]   [CloudDeployer]   [ExecutionChannel 抽象]
            │       aliyun│tencent│...     ├─ CloudAPIChannel(实现)
            │       CDN/DCDN/WAF/LB       ├─ K8sAPIChannel(实现, client-go)
       [alert/channel]   两段式上传+绑定    └─ BastionChannel/AgentChannel(接口预留)
            │                          [cert/cloudx 扩展]
       (webhook+email)           证书上传/列出/绑定 API 适配
            │
       [EIAM 权限/审计]          [crypto/x509 解析][crypto/tls 探测]
```

### Dependencies

| 依赖 | 类型 | 用途 |
|------|------|------|
| `k8s.io/client-go` | 新增 | K8s API Server 直连，CRD patch/读取 |
| `crypto/x509` / `crypto/tls` | 标准库 | 证书解析、SAN/链/有效期校验、TLS 握手探测 |
| `internal/shared/cloudx/{aliyun,tencent}` | 扩展 | 复用云账号凭证 + SDK，新增证书上传/列出/绑定方法 |
| `internal/task` | 复用 | 异步任务框架承载批量执行/扫描/探测 |
| `internal/alert` | 复用+扩展 | webhook + 邮件通道（channel 框架已存在） |
| `internal/audit` | 复用 | 变更全量审计记录 |
| `mongox` / Redis | 复用 | 持久化 + 限流缓存 |

## Interfaces

### Interface 1: ExecutionChannel（执行通道抽象）

```
type ExecutionChannel interface {
    Discover(ctx, creds Credential, scope DiscoverScope) ([]CertReference, error)
    Deploy(ctx, creds Credential, target DeployTarget, newCertFingerprint string) (DeployResult, error)
    Rollback(ctx, creds Credential, target DeployTarget, oldRef CertReference) (RollbackResult, error)
    Type() ChannelType  // "cloud_api" | "k8s_api" | "bastion" | "agent"
}
```
实现：`CloudAPIChannel`（含两段式 UploadCert+BindResource+CleanupOrphan）、`K8sAPIChannel`（CRD patch + 管理权探测 + 复检）。

### Interface 2: CloudDeployer（per 云 per 产品）

```
type CloudDeployer interface {
    UploadCert(ctx, creds, pem, key) (cloudCertId string, err error)   // 两段式第一段
    BindResource(ctx, creds, resourceId, cloudCertId) error             // 第二段
    ListReferences(ctx, creds, product) ([]CertReference, error)        // 只读发现
    CleanupOrphan(ctx, creds, cloudCertId) error                       // 孤儿清理
}
```
首期实现：阿里云 + 腾讯云的 CDN/DCDN/WAF/LB（6 deployer × 2 云）。

### Interface 3: ChangeService（变更单生命周期）

```
type ChangeService interface {
    GenerateChangeList(ctx, oldCertFingerprint, newCertId) (ChangeList, error)  // 指纹聚合+新鲜度校验+SAN预检
    Confirm(ctx, orderId, batchConf) error                                       // 快照确认时点重校验
    Execute(ctx, orderId) error                                                  // 派发子任务逐项执行
    Rollback(ctx, orderId, itemIds) error                                         // 仅成功项，前置目标有效性校验
    GetReport(ctx, orderId) (ChangeReport, error)
}
```

## Data Models

> DB-Schema: yes — 完整设计见独立文件。
> **ER Diagram**: design/er-diagram.md
> **SQL Schema**: design/schema.sql

### Field Quick Reference

| Model | Key Fields | Notes |
|-------|------------|-------|
| Certificate | fingerprint(唯一)、sans[]、issuer、notBefore/notAfter、hostingStatus、encryptedPrivateKey{ciphertext,keyVersion,algo}、protectUntil | 私钥信封加密 AES-256-GCM |
| CertReference | certFingerprint、cloud、product、clusterId、resourceId、referencedCloudCertId、snapshotId | 引用扫描发现 |
| ScanSnapshot | startedAt、finishedAt、coverageMeta[]{cloud,product,covered,total}、status | 新鲜度+覆盖率分母来源 |
| ChangeOrder | oldCertFingerprint、newCertId、status(8态)、batchInfo、snapshotId、verifyWindowUntil、protectUntil、creator | 状态机见 PRD |
| ChangeItem | orderId、resourceRef、action、oldCloudCertId、newCloudCertId、status、error | 逐项执行 |
| CloudCertMapping | certFingerprint、cloud、accountKey、cloudCertId、status(active/orphan) | 两段式/回滚/孤儿清理 |
| ProbeResult | domain、probeAt、onlineFingerprint、status(consistent/diff/unreachable/exempt) | TLS 探测 |
| Exemption | domain、reason、operator、createdAt | 探测豁免 |
| AlertConfig | webhookUrls[]、emailGroup[]、channelConfirmed、thresholds | 单文档 |
| K8sCredential | clusterName、kubeconfig(encrypted,keyVersion)、apiEndpoint | K8s 接入 |

## Error Handling

### Error Types & Codes

| Error Code | Name | Description | HTTP Status |
|------------|------|-------------|-------------|
| CERT_KEY_MISMATCH | KeyMismatchError | 证书与私钥不匹配 | 400 |
| CERT_CHAIN_INCOMPLETE | ChainIncompleteError | 证书链缺失 | 400 |
| CERT_PARSE_FAIL | ParseError | SAN 结构无法解析/已过期 | 400 |
| CERT_DUPLICATE_FINGERPRINT | DuplicateFingerprintError | 重复指纹导入 | 409 |
| SCAN_STALE | ScanStaleError | 扫描超新鲜度阈值，清单生成阻断 | 409 |
| SAN_INSUFFICIENT | SanInsufficientError | 新证书 SAN 不⊇目标域名 | 409 |
| CHANGE_IN_FLIGHT | ChangeInFlightError | 同一旧证书存在在途变更单 | 409 |
| ROLLBACK_TARGET_INVALID | RollbackTargetInvalidError | 旧证书已删除/过期，转人工 | 409 |
| CLOUD_API_RATELIMITED | CloudRateLimitedError | 云 API 限流，退避重试中 | 503 |
| K8S_UNREACHABLE | K8sUnreachableError | 集群不可达 | 503 |

### Propagation Strategy

- web 层捕获 domain error → 映射 HTTP 状态码 + 错误码返回；不泄露内部细节
- service 层：业务校验失败（scan_stale/san/互斥）返回明确语义错误供前端分支处理
- deployer 层：云 API 限流/K8s 不可达 → 子任务标记失败状态 + 原因，不中断其他项
- 私钥相关错误不得在 message 中携带私钥片段

## Cross-Layer Data Map

| Field Name | Storage Layer | Backend Model | API/DTO | Frontend Type | Validation Rule |
|------------|---------------|---------------|---------|---------------|-----------------|
| fingerprint | string,索引唯一 | Certificate.Fingerprint | json:"fingerprint" | string (mono) | SHA256 hex |
| encryptedPrivateKey | {cipher,keyVer,algo} | Certificate.EncryptedKey | 不返回 | 仅显示"已加密托管" | 永不外泄 |
| hostingStatus | string | Certificate.HostingStatus | json:"hostingStatus" | enum 完整/仅指纹 | complete/fingerprint_only |
| scanFreshness | int hours | ScanSnapshot.StartedAt | json:"lastScanAt" | 相对时间 | >24h 阻断清单 |
| changeStatus | string | ChangeOrder.Status | json:"status" | 8 态徽章 | 状态机约束 |
| probeStatus | string | ProbeResult.Status | json:"probeStatus" | enum 4 值 | consistent/diff/unreachable/exempt |
| coverageMeta | []{cloud,cov,total} | ScanSnapshot.CoverageMeta | json:"coverage" | 卡片 | 分母=资产同步独立盘点 |

## Integration Specs

No existing-page integrations — not applicable.（全部为新页面，e-cam-web 新增 `views/modules/cert/` 路由模块，沿用现有导航与权限控制机制。）

## Testing Strategy

### Per-Layer Test Plan

| Layer | Test Type | Tool | What to Test | Coverage Target |
|-------|-----------|------|--------------|-----------------|
| domain | 单元 | go test | 证书解析/校验/状态机转换/指纹聚合键 | 85% |
| deployer | 集成（mock SDK） | go test + mock | 两段式上传/绑定/孤儿清理/限流退避/管理权探测 | 80% |
| service | 集成 | go test + mongox test | 变更单全生命周期/回滚语义/新鲜度阻断/SAN 预检 | 85% |
| web | API 契约 | httptest | 端点契约/权限/错误码/私钥不外泄 | 80% |
| probe | 集成 | go test + 本地 TLS server | SNI/多证书/不可达/豁免 | 80% |

### Key Test Scenarios

- 完整性校验四项拦截（不匹配/链缺失/解析失败/过期）
- 变更清单：扫描超期阻断、SAN 不⊇拦截、在途互斥阻断
- 批量执行：部分失败→失败项保留旧引用→成功项回滚→回滚目标无效转人工
- 两段式第二段失败→孤儿证书补偿清理
- K8s CRD 管理权探测→GitOps 管理标记不可自动变更→变更后复检 reconcile 回写
- TLS 探测：线上≠台账差异告警、不可达标记、豁免不告警
- 私钥永不外泄：API/日志/报告/前端均无明文（渗透式自查）

### Overall Coverage Target

80%

## Security Considerations

### Threat Model

- **私钥集中托管**：平台成为高价值目标，DB 泄露 + 主密钥泄露 = 全网私钥失守
- **误操作/恶意批量更换**：生产 CDN/WAF/LB 被错误证书替换
- **云侧凭证滥用**：上传到云证书库的能力可被用于注入恶意证书
- **审计绕过**：变更无记录导致事后无法追溯
- **K8s 凭证泄露**：kubeconfig 可直连集群 API Server

### Mitigations

- 私钥：AES-256-GCM 信封加密，主密钥从环境变量注入独立于业务数据，`keyVersion` 支持轮换；仅在解析/校验/上传云证书库的内存中解密，用后 `zeroing`，不落盘/不进日志与报告
- 接口/前端永不返回明文私钥（渗透式自查口径：grep 全代码库无明文私钥返回点）
- 变更：人工确认 + 完整性前置校验 + 指纹精确匹配 + 扫描新鲜度 + 分批灰度 ≤50% + 回滚兜底 + EIAM 权限收敛 + 全量审计
- 云凭证：复用现有云账号 AK/SK 加密存储，deployer 操作限缩到证书相关 API
- K8s 凭证：kubeconfig 加密存储（同私钥加密体系），最小 RBAC（仅目标 CRD 读写）
- EIAM 三角色：运维工程师读写、运维主管/审计审计+配置、只读查看者看板只读；操作全量审计仅追加不可修改

## PRD Coverage Map

| PRD Requirement / AC | Design Component | Interface / Model |
|----------------------|------------------|-------------------|
| 证书托管台账+完整性校验 | CertService + IntegrityService | Certificate model, CERT_* errors |
| 引用关系发现 | ReferenceDiscoveryService + ExecutionChannel.Discover | CertReference, ScanSnapshot |
| 到期监控告警 | ProbeService + alert/channel | ProbeResult, AlertConfig |
| TLS 主动探测 | ProbeService (crypto/tls Dial) | ProbeResult |
| 一键批量更换 | ChangeService | ChangeOrder, ChangeItem |
| 回滚语义 | ChangeService.Rollback | ROLLBACK_TARGET_INVALID |
| 变更后验证窗口 | ChangeService verify stage | ChangeOrder.VerifyWindowUntil |
| 两段式+孤儿清理 | CloudDeployer | CloudCertMapping |
| K8s CRD 更新+管理权探测 | K8sAPIChannel | K8sCredential, change item status |
| 执行通道抽象 | ExecutionChannel | 接口预留 bastion/agent |
| 权限审计 | EIAM + internal/audit | audit logs |
| 前端页面 | e-cam-web cert 模块 | ui-design.md 已定型 |

## Open Questions

- [ ] 阿里云/腾讯云各产品证书 API 能力差异需 PoC 验证（列为首批任务）
- [ ] K8s 集群网络可达性（平台→APIServer）需逐集群确认
- [ ] 告警渠道（webhook/邮件 SMTP）凭据来源待配置

## Appendix

### Alternatives Considered

| Approach | Pros | Cons | Why Not Chosen |
|----------|------|------|----------------|
| 云 KMS 托管主密钥 | 安全等级高 | 依赖云 KMS 可达性+延迟+成本 | 选本地信封加密，无外部依赖、部署简单；运维妥善保管+备份主密钥 |
| 自研 Nginx Agent | 能力强 | agent 分发/升级/安全维护成本高 | 选 TLS 探测零侵入覆盖监控，更换走可插拔通道（堡垒机→优维 Agent） |
| 全自动无人工换证 | 效率最高 | 证书文件错误全网播撒 | 选一键批量+人工确认+回滚 |

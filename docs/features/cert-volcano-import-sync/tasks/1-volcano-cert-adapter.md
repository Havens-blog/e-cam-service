---
id: "1"
title: "火山云证书库发现适配器"
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

# 1: 火山云证书库发现适配器

## Description

火山引擎证书管理（volcengine-go-sdk v1.2.9 `certificateservice`）接入 e-cam 证书发现导入管线：实现云侧证书库的 List（实例清单）与 Get（证书链下载→指纹/SAN 解析），使火山在库证书可被导入台账。这是「火山云证书发现导入 + 定时增量同步」的地基——同步服务枚举全部证书可达云时，火山必须能供货源。

## Reference Files

- `docs/proposals/cert-volcano-import-sync/proposal.md` — 火山适配器（Proposed Solution / In Scope / Key Risks / Success Criteria）
- `internal/shared/cloudx/aliyun/cert.go` — 阿里云证书库适配器模式参考（List/Get + 指纹解析口径）
- `internal/shared/cloudx/volcano/lb.go` — 火山 SDK 装配与账号凭证使用模式参考
- `internal/shared/cloudx/volcano/adapter.go` — 火山适配器 init 注册/NewAdapter 模式参考

## Acceptance Criteria

- [ ] `ListCertificates` 分页枚举全部实例（fake SDK 数据断言：页数、实例数、字段透传与真实分页语义一致）
- [ ] `GetCertificate` 由 `Chain`（PEM 列表）解析出指纹（SHA256 小写 hex）/CN/SAN/有效期，与既有 CAS 口径（ParseCertAndKey 语义）一致
- [ ] revoked（`IsCertificateRevoked=true`）或非已签发 status 的实例在 List/Get 阶段过滤，不供导入
- [ ] 单实例失败（Get 错误/链解析失败）返回已获取部分 + 错误，不中断 List 后续条目
- [ ] `PrivateKey` 字段不进入返回结构、不进日志（Hard Rule：私钥只读后即丢）
- [ ] 只依赖 volcengine-go-sdk v1.2.9 既有 `certificateservice` 包，不新增任何依赖

## Hard Rules

- 私钥（PrivateKey 响应字段）在适配器内不存储、不进入任何返回类型、不进入日志/错误信息。
- 适配器为纯只读：仅 List/Get，绝不调用云写方法。
- 错误信息不得含云侧细节明文之外的敏感材料（对齐既有 wrapCertCloudErr 归一语义）。

## Implementation Notes

### Key Risks 缓解

- SDK 分页/字段形态不确定性：Get 依赖最小字段集（`Chain`/`FingerPrintSha256`/`San`/`Status`/`IsCertificateRevoked`）；List 分页字段以 SDK 模型为准文档化（`Skip`/`Limit` 或游标），fake SDK 严格按 `volcengine-go-sdk` 模型生成，避免假模型与真实结构漂移。
- 证书链缺根：CAS 链常只存 leaf+中间，导入侧 checkChain 已支持系统信任库兜底（644b067），适配器不必自行补根。

### File Scope

新增：`internal/shared/cloudx/volcano/cert.go`、`internal/shared/cloudx/volcano/cert_test.go`（fake SDK，标准库 httptest 或 stub service 客户端）。

### Test Impact

- 新文件 + 新测试；不改动现有文件。风险低。
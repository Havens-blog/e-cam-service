---
id: "4"
title: "发现导入服务编排"
priority: "P0"
estimated_time: "2h"
complexity: "high"
dependencies: [1, 2]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 4: 发现导入服务编排

## Description

发现导入核心编排：会话先持久化再异步执行；逐条 GetCert（净化 PEM）→解析→指纹登记（fingerprint_only）→CloudCertMapping 幂等建档→占位指纹引用回填；ErrDuplicateFingerprint 转补建映射记 success；单条失败/panic 不中断；整体限时 10 分钟；终态收敛 completed/partial_failed。云凭证按账号解析复用扫描链路 ScanAccountSource.ActiveByCloud 模式（会话生命周期长于 HTTP 请求，凭证需在会话内按账号获取）。

## Reference Files
- `docs/proposals/cert-cloud-discovery-import/proposal.md` — In Scope 会话端点条目、Key Scenarios（重复执行/占位指纹/多账号/云侧删除）、Success Criteria SC-4/SC-5/SC-6/SC-9 (ref: In Scope; Key Scenarios; Success Criteria)
- `internal/cert/service/import_service.go`: 复用 ImportCert 落库路径（keyPEM 空分支即 fingerprint_only）、参照 runBatch 异步会话/panic 隔离/限时模式
- `internal/cert/service/reference_scan_service.go`: ScanAccountSource.ActiveByCloud 凭证来源模式；占位指纹公式 certscan-unresolved:{cloud}|{accountKey}|{certId}
- `internal/cert/domain/cloud_cert_mapping.go`: Upsert 幂等建档（uk_fp_cloud_account 两段去重）+ GetByFingerprint
- `internal/cert/certtest/`: fake 适配器/仓储注入的测试基建

## Acceptance Criteria

- [ ] SC-4：会话先持久化再异步执行（浏览器中断不丢结果）；逐条处理链 GetCert→净化→解析→登记（fingerprint_only + EncryptedPrivateKey 为空）→映射建档；单条失败记 errorReason（静态文案，不携带云响应片段）不中断后续；GetCert Exists=false 记因"云侧已不存在"跳过
- [ ] SC-5：同指纹重放不产生重复台账记录——捕获 ErrDuplicateFingerprint 后 GetByFingerprint 取既有证书、Upsert 本云本账号映射、条目记 success（"已在台账，已补建映射"）；会话终态按失败计数收敛 completed/partial_failed；整体限时对齐批量导入 10 分钟口径
- [ ] SC-6：成功条目按 (cloud,accountKey,cloudCertId) 将 cert_references 中仍为占位指纹的引用批量回填为真实指纹（占位公式派生值才回填；真实指纹引用永不被覆盖；回填以导入时点 GetCert 为准）；验收测试含腾讯 SHA-1 回退样本；多账号同证书仅 1 台账记录且映射按账号各 1 条
- [ ] SC-9：内容级断言（单测）：入库 CertPEM 不含 "PRIVATE KEY" 字样、含且仅含 CERTIFICATE 块（叶在前 fullchain）；hostingStatus=fingerprint_only
- [ ] 华为云/IAM-hosted 条目进入导入请求时记因跳过（不调云 API）；单条 panic 由 recover 兜底记 INTERNAL_ERROR 静态文案，不中断会话

## Hard Rules

- 单条失败/panic 不中断会话（对齐批量导入 Hard Rule）
- 私钥不落库为构造性保证：导入路径仅写 CertPEM/指纹及解析字段；errorReason 静态文案不携带云响应片段与 panic 值

## Implementation Notes

- 回填语义：占位引用仅指向 cloudCertId 本身，回填一律以导入时点 GetCert 为准（ACM 续期保留 ID/ARN 时回填现行证书指纹，非误写）；误回填可由重扫按确定性公式重建（可恢复性）
- 并发会话与重扫描同时写 cert_references 指纹：占位指纹为确定性可重算值，写竞争良性，无需互斥（同值幂等）
- 会话级并发（两个导入会话重叠）本期不做互斥：条目级幂等（uk_fingerprint + 映射 Upsert）保证结果一致，进度视图各自独立

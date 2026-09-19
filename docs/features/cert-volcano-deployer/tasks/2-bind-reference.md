---
id: "2"
title: "火山部署器绑定层（BindResource/ListReferences × 4 产品）"
priority: "P0"
estimated_time: "2h"
complexity: "high"
dependencies: ["1"]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 2: 火山部署器绑定层（BindResource/ListReferences × 4 产品）

## Description

火山部署器第二段 + 只读发现：`BindResource` 按 4 产品绑定新云证书（CDN `BatchDeployCert` 加速域名 / WAF 域名证书替换 / ALB 监听证书 / NLB 监听证书），`ListReferences` 枚举四产品资源→`CertReference`（指纹解析对齐 3.5：映射反查 → GetCert 要素 → 确定性占位指纹 `certscan-unresolved:`）。绑定/发现的资源 ID 形态与引用粒度对齐既有 alb/nlb 监听复合 ID + served domains 展开先例。

## Reference Files

- `docs/proposals/cert-volcano-deployer/proposal.md` — Proposed Solution / Key Scenarios / Key Risks / Success Criteria
- `internal/cert/deployer/huawei_deployer.go` — 4 产品 BindResource/ListReferences 分支先例
- `internal/cert/deployer/channel.go` — CloudDeployer 端口（302-314 行）与 CertReference 形态
- `internal/cert/deployer/aws_deployer.go` — NLB 监听证书绑定 + served domains 展开先例

## Acceptance Criteria

- [ ] `BindResource` 按 product 分支：CDN=BatchDeployCert(加速域名)、WAF=域名证书替换、ALB=监听证书、NLB=监听证书；失败返回可识别错误（对齐 wrapCertCloudErr 哨兵）
- [ ] `ListReferences` 四产品资源枚举 → `CertReference`（指纹解析：映射反查→GetCert 要素→确定性占位），alb/nlb 监听按 served domains 展开（对齐 5 云口径）
- [ ] 幂等：同一 (resource, cloudCertID) 重绑结果收敛（对齐既有幂等重跑测试）
- [ ] fake SDK 单测覆盖：4 产品 × 绑定成功/失败、ListReferences 全产品枚举 + 占位指纹语义
- [ ] 与任务 1 的 `{product}:{id}` ID 归一互操作（绑定引用归一 ID 可被回滚 GetCert 解析）

## Hard Rules

- 仅修改以下文件：`internal/cert/deployer/volcano_deployer.go`、`internal/cert/deployer/volcano_deployer_test.go`（本任务追加 BindResource/ListReferences）。
- 绑定/发现为适配层职责：错误归一、ID 归一不与既有五云行为耦合（新增分支不破坏）。

## Implementation Notes

### Key Risks 缓解

- CDN 域绑定粒度（M/M）：引用扫描按域名粒度（resourceId=域名），BindResource 按加速域名；对齐 ListCdnCertInfo/DescribeCertConfig 语义。
- ALB/NLB 监听证书 API 幂等/替换（M/M）：对齐 aws NLB 监听证书先例 + served domains 展开；幂等重跑测试。

### File Scope

追加到任务 1 建立的 `volcano_deployer.go`（BindResource/ListReferences + 产品分支）+ 测试。

### Test Impact

- 新增绑定/发现用例；`TestAwsCloudFront_RebindIdempotentTerminalState` 等既有幂等断言模式参考，不破坏。风险 low。
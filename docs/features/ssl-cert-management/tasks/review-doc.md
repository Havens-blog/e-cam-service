---
id: "T-review-doc"
title: "Review Documentation Quality"
priority: "P1"
estimated_time: "30min"
dependencies: ["1.3", "5.12"]
type: "doc.review"
surface-key: ""
surface-type: ""
---

Review documentation quality for the ssl-cert-management feature (breakdown mode).

## Acceptance Criteria Summary

The following acceptance criteria are pre-extracted from doc tasks. Use these as the review baseline.

### 1.3-cert-schema-apply
- [ ] 在开发库执行 schema.sql 全文：12 个 `createCollection` 均成功，`$jsonSchema` 校验器生效。
- [ ] 全部 `createIndex` 成功：唯一索引（uk_fingerprint/uk_fp_cloud_account/uk_cluster_group_kind）、部分唯一索引（uk_active_mutex partialFilterExpression activeMutex 存在；idx_status_heartbeat partialFilterExpression status=running）、TTL 索引（ttl_probe_90d/ttl_batch_session_30d）均存在且定义正确。
- [ ] 越界文档被拒绝：如 `hostingStatus: "bogus"`、`fingerprint: "not-hex"`、`status` 非枚举值等样例被 `$jsonSchema` 拒绝并记录。
- [ ] 部分唯一索引互斥验证：插入两条 `activeMutex` 相同的活跃单 → 第二条 duplicate key；终态 `$unset` 后可再插同 token。
- [ ] `schema-apply-verification.md` 产出，含执行命令、索引清单、与 1.2 `EnsureIndexes` 实现的一致性比对结论（一致 / 偏差列表）。


### 5.12-cloud-cert-poc
- [ ] 阿里云真实账号验证：CDN/DCDN/WAF/ALB(NLB) 四类产品的 UploadCert→BindResource→GetCert→CleanupOrphan 全链路各至少一次成功，记录每步请求/响应要点（脱敏）。
- [ ] 腾讯云真实账号验证：CDN/EdgeOne(WAF)/CLB 三产品同上全链路验证。
- [ ] 限制与配额记录成表：证书命名/格式限制、证书库配额、上传频率限制、绑定粒度（如 CLB 监听器级）、限流阈值表现。
- [ ] 失败与限制场景记录：至少覆盖限流响应形态、格式拒绝、重复上传行为（幂等 or 报错）。
- [ ] `design/poc-notes.md` 产出且 tech-design Open Questions 对应项关闭；发现的代码偏差已回填 3.1/3.2（或登记为 5.4/5.5 修正项清单）。


## Discovery Strategy

Scan ONLY the following allowlist of directories for target documents:
- docs/features/ssl-cert-management/ (prd/, design/, testing/, and any subdirectories)
- docs/proposals/ssl-cert-management/

EXCLUDE the following from scanning — do NOT read or process these:
- tasks/ directory (task definitions are not deliverables)
- tasks/records/ directory (execution records are not deliverables)
- manifest.md (build artifact)
- index.json (build artifact)

Only .md files under the allowlist directories are target deliverables.

## Acceptance Criteria

- [ ] All acceptance criteria met

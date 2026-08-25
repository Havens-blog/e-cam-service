---
id: "3"
title: "发现预览与快照状态查询端点"
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

# 3: 发现预览与快照状态查询端点

## Description

两个只读 GET 端点：(1) GET /api/v1/certs/discovery/preview——基于最近 done 快照纯 DB 聚合唯一证书清单（去重公式排除 product=crd、空 cloud），双通道 inLedger 判定（台账指纹 或 CloudCertMapping FindByCloudCertID），不可解析类标记（华为/IAM-hosted/占位指纹）统一由可解析标记字段承载，无快照返回 NO_SNAPSHOT，响应携带 snapshotStartedAt；(2) GET /api/v1/certs/discovery/snapshot-status——返回最近快照 status/startedAt/partialFailures，供无快照引导轮询（现有路由面无任何状态查询端点，此为本期新增交付物，只读不改扫描编排）。

## Reference Files
- `docs/proposals/cert-cloud-discovery-import/proposal.md` — In Scope 预览端点与快照状态端点条目、Success Criteria SC-1/SC-2/SC-3/SC-7/SC-8 (ref: In Scope; Success Criteria)
- `internal/cert/service/reference_query_service.go`: 参照引用查询服务的仓储注入风格；快照/引用仓储读取
- `internal/cert/web/reference_handler.go`: 路由注册参照（现有 /reverse、/:id/references、POST /:id/scan）
- `internal/cert/web/router.go`: 新增 discovery 路由组（preview、snapshot-status 均挂 RequireRoles(RoleOpsEngineer)）
- `internal/cert/domain/cloud_cert_mapping.go`: FindByCloudCertID 双通道判定依据（certtest/change_fakes.go:754 接口签名）

## Acceptance Criteria

- [ ] SC-1：有 done 快照且台账空时，预览条目数 = 快照引用按（cloud+accountKey+cloudCertId）去重数（排除 product=crd 引用；空 cloud 条目不计入；含占位指纹条目与华为云/AWS IAM-hosted 不可选组），纯 DB 聚合无云 API 调用，响应 < 1s
- [ ] SC-2：每个条目含 cloud/accountKey/cloudCertId/引用资源数/inLedger（指纹或 FindByCloudCertID 双通道命中即 true）/notAfter（inLedger 为台账值，未登记为占位"—（导入后补全）"）/可解析标记 七类字段，响应另含 snapshotStartedAt；已在台账条目 inLedger=true
- [ ] SC-3 后端部分：无 done 快照时返回 NO_SNAPSHOT 结构化错误码（非 500）
- [ ] snapshot-status：返回最近快照 status（running/done/failed）/startedAt/partialFailures；零快照时空态响应有明确定义（区别于 NO_SNAPSHOT 的引导语义按实现注记）
- [ ] SC-8 部分：preview 与 snapshot-status 对非 OpsEngineer 角色 403（权限矩阵单测）；华为云整组不可选、AWS IAM-hosted 同语义降级（parseable=false 归入不可选组）

## Hard Rules

- 预览端点不得调用任何云 API（纯 DB 聚合是 SC-1 的硬约束）

## Implementation Notes

- notAfter 数据来源定案：inLedger 条目显示台账 NotAfter，未登记条目占位显示——cert_references 无 notAfter 字段且本功能不改其表结构
- IAM-hosted 判定提示：AWS 非 ARN 形态（无 "arn:" 前缀）可由后端按前缀判定打 parseable=false 标记
- 双通道 inLedger 的意义：占位指纹条目重跑预览时靠映射通道被正确灰选（"重跑仅处理剩余项"的预览期呈现）

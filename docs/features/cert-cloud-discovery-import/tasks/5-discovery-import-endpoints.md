---
id: "5"
title: "发现导入会话端点与权限矩阵"
priority: "P1"
estimated_time: "1h"
complexity: "medium"
dependencies: [4]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 5: 发现导入会话端点与权限矩阵

## Description

web 层接线：POST /api/v1/certs/discovery/import（勾选条目创建会话，202 语义返回 sessionId）+ 会话进度 GET（轮询数据源），挂 RequireRoles(RoleOpsEngineer)；补齐全功能权限矩阵单测（preview/snapshot-status/import/progress 四端点 × 角色）。

## Reference Files
- `docs/proposals/cert-cloud-discovery-import/proposal.md` — In Scope 会话端点条目、Success Criteria SC-8 (ref: In Scope; Success Criteria)
- `internal/cert/web/cert_handler.go`: ImportBatch 202 语义与 BatchVO 同构响应模式参照
- `internal/cert/web/router.go`: discovery 路由组（任务 3 建立）追加 import/progress
- `internal/cert/web/authz_matrix_test.go`: 权限矩阵测试追加四端点行
- `internal/cert/module.go`: 服务装配（任务 4 服务注入 handler）

## Acceptance Criteria

- [ ] POST /api/v1/certs/discovery/import：请求体为勾选条目清单（cloud/accountKey/cloudCertId），创建会话返回 202 语义（sessionId）；空清单返回结构化错误
- [ ] GET 会话进度：返回会话状态/进度计数/逐条结果（result/errorReason/mappedCertID），终态 completed/partial_failed 可判
- [ ] SC-8：权限矩阵单测覆盖 preview/snapshot-status/import/progress 四端点——非 OpsEngineer 角色 403、OpsEngineer 通过

## Implementation Notes

- 响应 VO 与批量导入 BatchVO 同构风格（batchId/状态/进度/条目），字段名对齐会话实体（任务 2）
- 端到端 fake 测试（fake 适配器 + fake 仓储）验证多账号同证书场景：1 台账记录 + 按账号各 1 映射

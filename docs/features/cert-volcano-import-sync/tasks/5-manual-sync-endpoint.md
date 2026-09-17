---
id: "5"
title: "手工触发同步入口"
priority: "P1"
estimated_time: "1h"
complexity: "medium"
dependencies: ["3"]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 5: 手工触发同步入口

## Description

同步服务的手工触发 HTTP 入口：`POST /discovery/sync`——与定时轮共享同一服务与 CAS 防重守卫，供天级调度前试跑与运维按需触发。返回同步轮结果摘要，进度可经既有会话查询端点轮询。

## Reference Files

- `docs/proposals/cert-volcano-import-sync/proposal.md` — 手工触发入口（In Scope / Key Scenarios / NFR 可观测）
- `internal/cert/web/discovery_handler.go` — 既有 `POST /discovery/import` 端点模式（会话创建/返回/轮询/鉴权）
- `internal/cert/service/cert_sync_service.go` — 任务 3 同步服务（同一实现被本端点与定时轮共用）

## Acceptance Criteria

- [ ] `POST /discovery/sync`（`RequireRoles(RoleOpsEngineer)`）触发一轮同步，返回同步轮 ID / 会话摘要（对齐 `/discovery/import` 响应形态）
- [ ] 与定时轮共享同一服务与 CAS 守卫：同步已在跑 → 409（`ErrSyncRunning` 归一语义），不排队不吞错
- [ ] 路由/handler 单测：鉴权（非 RoleOpsEngineer 拒绝）、成功触发、running 冲突 409
- [ ] 同步轮结束响应不泄漏单云失败细节（仅错误类别/reason 摘要，对齐 NFR「云侧错误细节不进响应」）

## Hard Rules

- 仅修改以下文件：`internal/cert/web/discovery_handler.go`、`internal/cert/web/discovery_handler_test.go`（必要时补路由注册处）。

## Implementation Notes

### 端点形态对齐

- 响应/鉴权/错误码参照 `POST /discovery/import` 既有契约（避免前端两套契约语义）；同步轮为一次性结果摘要，无需新会话轮询端点（既有 `/discovery/import/:sessionId` 语义若复用请注明）。
- 同步轮若未产出任何新条目，响应仍 200（业务上是"已收敛"，非错误）。

### Test Impact

- handler 单测（鉴权/触发/409/泄漏性）新增；既有 discovery handler 测试全绿。风险 low。
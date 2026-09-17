---
status: "completed"
started: "2026-09-17 11:55"
completed: "2026-09-17 12:14"
time_spent: "~19m"
---

# Task Record: 5 手工触发同步入口

## Summary
手工触发同步入口 POST /api/v1/certs/discovery/sync 落地：DiscoveryHandler 新增 sync 端点（RequireRoles(RoleOpsEngineer)，消费 service.CertSyncService 的 Manual 面 SyncCertificatesManual，与定时轮共享同一服务与 CAS 防重守卫）；响应为一次性 SyncRun 同构摘要 CertSyncRunVO（200 信封形态对齐 /discovery/import，sessionId 非空时可复用既有 GET /discovery/import/:sessionId 轮询）；同步已在跑返回 409 CERT_SYNC_IN_PROGRESS（ErrSyncRunning 归一语义，handler 内 errors.Is 拦截，response.go 不在 Hard Rule 文件内故不动）；失败摘要仅白名单字段+静态 reason（NFR 云侧错误细节不进响应）。10 个新测试子用例（成功摘要/409 冲突/零导入收敛 200/失败摘要泄漏性/通用错误 500/未装配防御分支/viewer 403/未设置角色 403），既有 discovery handler 测试全绿。

## Changes

### Files Created
无

### Files Modified
- internal/cert/web/discovery_handler.go
- internal/cert/web/discovery_handler_test.go
- internal/cert/module.go

### Key Decisions
- NewDiscoveryHandler 以可选变参（sync ...service.CertSyncService）扩展签名：8 处 Hard Rule 清单外既有调用点（module.go+6 个 web 测试文件+tests/discoverytest/harness.go）零改动继续编译；直接改签名需改 8 个越权文件，被否决
- module.go:336 生产装配一行补注 certSyncSvc——Hard Rule 括注「必要时补路由注册处」的唯一必要越界（不注入则端点在生产为死代码，AC1 不成立）；恰好一行、纯装配
- 409 映射放 handler 内 errors.Is 而非 response.go mapError（后者不在允许文件清单）；错误码常量 CodeCertSyncInProgress 定义在 handler 本地（先例：settings_handler.go CodeCrdDuplicateRegistration）
- 同步执行为同步面：调用返回即终态，响应 200 一次性摘要（非 202 会话）；零新条目仍 200（业务已收敛）；不新增轮询端点，逐条导入结果复用既有 /discovery/import/:sessionId
- 未装配 sync 的路由形态防御分支返回 500 INTERNAL_ERROR 固定文案（既有测试路由均未传 sync，防 nil panic）

## Test Results
- **Tests Executed**: Yes
- **Passed**: 384
- **Failed**: 0
- **Coverage**: 79.8%

## Acceptance Criteria
- [x] POST /discovery/sync（RequireRoles(RoleOpsEngineer)）触发一轮同步，返回同步轮 ID/会话摘要（对齐 /discovery/import 响应形态）
- [x] 与定时轮共享同一服务与 CAS 守卫：同步已在跑返回 409（ErrSyncRunning 归一语义），不排队不吞错
- [x] 路由/handler 单测：鉴权（非 RoleOpsEngineer 拒绝）、成功触发、running 冲突 409
- [x] 同步轮结束响应不泄漏单云失败细节（仅错误类别/reason 摘要）

## Notes
覆盖率：新增代码 Sync=100%、toCertSyncRunVO=100%（go tool cover -func）；web 包总 79.8% 为存量基线，新增代码全覆盖。testsPassed=web 包 378 + internal/cert 根包 6（module 装配回归），均 -v 实数，0 FAIL 0 SKIP。宿主无 cgo，-race 不可用（既有约束）。fmt：两改动的 web 文件（LF）gofmt -l 干净；module.go 为存量 CRLF 全文件标记，经 tr -d '\r' 临时文件法核真实 delta=0 字节，按既有约定放行记 WARNING。U+FFFD 事件：Edit 写 CertSyncRunVO 注释时 1 处中文字被写成 3×U+FFFD，已按坑 7 流程 node 脚本按 U+4F46 重建并全文复扫三文件=0 残留。Static checks：go build -p 1 ./... 通过；go vet ./internal/cert/... 通过；authz_matrix_test.go 为硬编码端点清单不受新路由影响（全量 web 包绿实证）。

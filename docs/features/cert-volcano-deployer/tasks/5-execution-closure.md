---
id: "5"
title: "两段式/验证/回滚/孤儿清理火山接通验证"
priority: "P1"
estimated_time: "2h"
complexity: "medium"
dependencies: ["4"]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 5: 两段式/验证/回滚/孤儿清理火山接通验证

## Description

火山部署器接入既有编排后的闭环验证：两段式执行（UploadCert→BindResource→映射 active）、失败补偿（CleanupOrphan 幂等 + 映射 active→orphan）、验证窗口（ProbeDomains 判定线上=新指纹，云无关）、回滚（GetCert 校验+反绑恢复旧 ID）。全部复用既有状态机，本任务以 fake SDK 桩做火山接通的功能级验证（对齐 cert-multicloud-deployers 的 journey/matrix 测试形态），并暴露任何编排层与火山 ID 归一的不匹配。

## Reference Files

- `docs/proposals/cert-volcano-deployer/proposal.md` — Success Criteria（SC4-SC7）/ Key Scenarios / Next Steps
- `internal/cert/service/execute_service.go` — 两段式编排 + 失败补偿（batchActionRank/补偿语义）
- `internal/cert/service/verify_window_service.go` — ProbeDomains 验证窗口（391 行复用点）
- `internal/cert/service/rollback_service.go` — GetCert 三判定回滚
- `tests/multicloud-cert-replacement/` — 三云替换 journey 测试先例（矩阵形态）

## Acceptance Criteria

- [ ] 两段式：火山 4 产品 UploadCert→BindResource 成功 → 映射写入 active（`{product}:{id}` 形态断言）
- [ ] 失败补偿：绑定失败 → `CleanupOrphan` 幂等（双调用同结果）+ 映射 `active→orphan` 入清理队列
- [ ] 验证窗口：火山目标域名经 `ProbeDomains` 判定线上指纹=新证书（云无关复用断言）
- [ ] 回滚：`GetCert` 校验旧 ID 有效 → `BindResource` 恢复旧 ID（4 产品各一用例）
- [ ] 与任务 4 装配联调：编排层按 product 分发到火山部署器正确（注册可见性断言）

## Hard Rules

- 仅修改以下文件：`internal/cert/service/` 测试文件或 `tests/` 下本 feature journey 测试（对齐 multicloud-cert-replacement 形态）。
- 测试为功能级验证：不新增编排机制，只验证火山接通既有状态机。

## Implementation Notes

### Key Risks 缓解

- 编排层与火山 ID 归一的隐式耦合：本任务暴露即修——若 `{product}:{id}` 与编排层的 normalize/rollback 预期冲突，以测试断言定位并记录偏差（优先对齐既有 normalize 模式，不改编排核心）。

### File Scope

新增：`internal/cert/service/volcano_closure_test.go` 或 `tests/<journey>/` 功能测试（对齐既有 tests/multicloud-cert-replacement 矩阵先例）。

### Test Impact

- 新增火山接通用例；既有编排/验证/回滚测试全绿回归。风险 medium（编排层一旦暴露火山归一不匹配需小修适配层，不触编排核心）。
---
id: "2"
title: "发现导入会话实体与仓储"
priority: "P0"
estimated_time: "1h"
complexity: "medium"
dependencies: []
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 2: 发现导入会话实体与仓储

## Description

发现导入的后端会话载体（后端独立改造项，不与前端进度组件复用混同）：新集合（或对既有批量会话条目泛型化），条目字段 cloud/accountKey/cloudCertId/result/errorReason/mappedCertID，支撑"先持久化再异步执行（浏览器中断不丢结果）"与终态收敛 completed/partial_failed。

## Reference Files
- `docs/proposals/cert-cloud-discovery-import/proposal.md` — In Scope 会话实体条目、Non-Functional Requirements 可靠性条目 (ref: In Scope; Non-Functional Requirements)
- `internal/cert/domain/batch_session.go`: 参照 CertBatchSession 形态建模（新集合或条目泛型化，二选一在实现期定，保持与批量会话模型风格一致）
- `internal/cert/domain/repository.go`: 新增会话仓储接口（Create/GetByID/RecordItemResult/MarkFinished）
- `internal/cert/repository/`: mongo 实现 + EnsureIndexes 启动建索引；`internal/cert/certtest/`: fake 仓储供服务层测试

## Acceptance Criteria

- [ ] 会话文档含条目（cloud/accountKey/cloudCertId/result/errorReason/mappedCertID）、进度计数（total/succeeded/failed）、operator、status（running/completed/partial_failed）、时间戳字段
- [ ] 仓储接口与 mongo 实现就绪：Create（持久化即返回 ID）、GetByID、RecordItemResult（按条目索引记结果与原因）、MarkFinished（按失败计数收敛终态）
- [ ] EnsureIndexes 注册（启动自动建索引）+ certtest fake 仓储实现（供任务 4/5 服务层与端点测试注入）

## Implementation Notes

- 判定"新集合 vs 泛型化 CertBatchSession"以改动面最小为准：批量会话条目以 FileName 为主键语义（展示与错误定位），发现导入条目是三元组——伪装字段（cloudCertId 塞 FileName）不允许
- 会话整体限时 10 分钟口径由任务 4 编排层承担（对齐 batchProcessTimeout），实体只承载状态

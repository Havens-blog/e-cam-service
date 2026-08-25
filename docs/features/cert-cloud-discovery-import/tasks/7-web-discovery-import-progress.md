---
id: "7"
title: "前端：导入进度交互与完成刷新"
priority: "P1"
estimated_time: "1h"
complexity: "medium"
dependencies: [5, 6]
surface-key: ""
surface-type: ""
breaking: false
type: "coding.feature"
mainSession: false
---

# 7: 前端：导入进度交互与完成刷新

## Description

确认导入后的会话进度交互：POST /discovery/import → 轮询 GET 会话进度（复用批量导入会话进度交互模式，不改其内部实现）→ 终态展示（completed/partial_failed，逐条结果与失败原因可见）→ 完成刷新台账列表。

## Reference Files
- `docs/proposals/cert-cloud-discovery-import/proposal.md` — 用户旅程进度段、Key Scenarios 首次登记主路径、Success Criteria SC-4/SC-5 前端侧 (ref: Proposed Solution; Key Scenarios; Success Criteria)
- `e-cam-web/src/views/cert/ledger/components/BatchImportModal.vue`: 进度轮询交互模式参照（独立实现，不改其内部）
- `e-cam-web/src/views/cert/ledger/index.vue`: 完成后列表刷新挂接

## Acceptance Criteria

- [ ] 预览 Modal 确认勾选后调用 POST /discovery/import，切换为进度视图轮询会话 GET（间隔与批量导入一致）
- [ ] 进度视图展示进度计数与逐条结果；partial_failed 终态逐条失败原因（errorReason）可见
- [ ] 终态（completed/partial_failed）停止轮询；完成后刷新台账列表（新增登记项立即可见）
- [ ] vitest 用例覆盖：轮询-终态收敛、失败原因展示、完成刷新触发

## Implementation Notes

- 浏览器中断不丢结果由后端会话持久化保证，前端重开 Modal 按 sessionId 恢复查询即可（不做复杂恢复 UI，重跑入口保留）

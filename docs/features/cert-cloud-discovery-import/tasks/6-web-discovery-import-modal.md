---
id: "6"
title: "前端：从云端导入入口与预览 Modal"
priority: "P1"
estimated_time: "2h"
complexity: "high"
dependencies: [3]
surface-key: ""
surface-type: ""
breaking: false
type: "coding.feature"
mainSession: false
---

# 6: 前端：从云端导入入口与预览 Modal

## Description

e-cam-web 台账页新增"从云端导入"入口（空态 CTA 增强 + 工具栏按钮）与预览 Modal：分组列表、已在台账灰选、不可选组提示（华为云/AWS IAM-hosted）、快照超 7 天重扫提示、notAfter 未登记占位显示、默认全选未登记项。前置：从 feat/platform-user-management 基线拉出功能分支（该分支需先 push/merge，见提案 Constraints）。

## Reference Files
- `docs/proposals/cert-cloud-discovery-import/proposal.md` — In Scope 前端条目、用户旅程、Success Criteria SC-1/SC-2/SC-7 前端侧 (ref: Proposed Solution; In Scope; Success Criteria)
- `e-cam-web/src/views/cert/ledger/index.vue`: 空态 CTA 增强（"从云端导入存量证书"）+ 工具栏按钮
- `e-cam-web/src/views/cert/ledger/components/BatchImportModal.vue`: 参照 Modal 结构风格；新组件独立（DiscoveryImportModal.vue），不修改其内部实现
- `e-cam-web/src/views/cert/ledger/`: API 封装（discovery preview/snapshot-status/import/progress 客户端函数）

## Acceptance Criteria

- [ ] 台账页双入口（空态 CTA + 工具栏按钮）打开预览 Modal 并加载 GET /discovery/preview
- [ ] 预览列表按云分组展示条目七字段；已在台账（inLedger）灰选不可勾；华为云/AWS IAM-hosted 不可选组带"暂不支持自动解析"提示；默认勾选全部未登记可选项
- [ ] 快照超 7 天（snapshotStartedAt 计算）顶部显著提示建议重扫；notAfter 未登记条目显示"—（导入后补全）"
- [ ] NO_SNAPSHOT 错误码状态触发引导入口（引导流程在任务 8 实现，本任务暴露触发点与占位交互）
- [ ] vitest 新增组件用例；既有 289 用例全绿（回归以不改既有组件内部实现为前提）

## Hard Rules

- 进度/批量导入既有组件仅复用不改内部实现；新增交互一律独立组件

## Implementation Notes

- 分支顺序：先 push/merge feat/platform-user-management 到远端共享分支，再拉本功能分支（提案 Dependency Readiness 缺口 (2)）
- 预览 API 形状以后端任务 3 的响应为准（七字段 + snapshotStartedAt）；前端类型定义与之对齐
- Modal 分组渲染注意大清单性能（分组折叠即可，无需虚拟滚动）

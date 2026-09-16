---
id: "2"
title: "查询进行中状态 + 失败可重试(消除 20s 白屏无回馈)"
priority: "P1"
estimated_time: "1h"
complexity: "low"
dependencies: []
surface-key: ""
surface-type: ""
breaking: false
type: "coding.enhancement"
mainSession: false
---

# 2: 查询进行中状态 + 失败可重试(消除 20s 白屏无回馈)

## Description
当前 `doSearch` 期间仅骨架屏,长时间无文字回馈;单源失败只有 sources-strip 标签;整页失败给原始错误串无引导。改为:查询进行中显示"正在查询 N 个云账号 · 已耗时 Xs"进度态(文案文本化 + aria-label);失败区提供可点击重试与可读原因;超时/截断给明确降级说明。不改变现有接口契约。

## Reference Files
- `docs/proposals/log-query-optimization/proposal.md` — Source proposal;«Problem» «Key Scenarios» «Success Criteria»
- `e-cam-web/src/views/logs/index.vue`: doSearch/searching/searchError 状态与结果区

## Acceptance Criteria
- [ ] 查询期间(含多源翻页)显示文字进度态:查了 N 个源/已耗时,非纯骨架
- [ ] 全失败态有重试按钮与可读原因(不再是裸错误串)
- [ ] 部分源失败时的 per-source 标签保持,且整页不进入失败态
- [ ] 状态/失败文案文本化(非仅颜色),等待态有 `aria-label`
- [ ] vue-tsc / eslint 通过;查询/翻页/聚合行为无回归

## Implementation Notes
- 无需后端改动(响应已含 per-source `sources[].error` 与 `duration_ms`,前端已有 data)。
- 进度态可在 `searching=true` 后按已返回的 sources 计数或一个递增计时器显示已耗时;不必轮询真实进度。
- 失败重试复用 `doSearch`;错误串映射为中文(截断/超时/未投递)。
- 保持既有「明细默认折叠、统计视图为主」布局不动。
---
id: "4"
title: "字段筛选快捷值回填(从已返回样本点选常见值)"
priority: "P1"
estimated_time: "1.5h"
complexity: "medium"
dependencies: []
surface-key: ""
surface-type: ""
breaking: false
type: "coding.feature"
mainSession: false
---

# 4: 字段筛选快捷值回填(从已返回样本点选常见值)

## Description
字段筛选目前全手输值(状态码/IP/域名)。新增「快捷值」:当前已返回样本(allEntries)里按字段自动聚合常见值(状态码集合、Top 域名、动作/命中枚举、Top IP),展示为可点选 chips,点击填充到筛选行的 value 并可直接重查。零额外请求(复用已加载样本),频率降序、去重、上限 8 个。更新时间/类型/筛选变化时重算。

## Reference Files
- `docs/proposals/log-query-optimization/proposal.md` — Source proposal;«Proposed Solution» 4 条 fav «Key Scenarios»
- `e-cam-web/src/views/logs/index.vue`: fieldFilters/filterableFields/allEntries/buildFilters
- `e-cam-web/src/views/logs/format.ts`: cellValue(从 entry 取字段值的既有辅助)

## Acceptance Criteria
- [ ] 每行筛选字段选择后,下方展示该字段的快捷值 chips(来自 allEntries 样本,频率降序去重,≤8)
- [ ] 点击 chip 填入该行 value;再次点击取消(可 toggle)
- [ ] 快捷值覆盖 状态码/域名/动作/缓存命中/严重度/IP(至少这些常见字段口径一致,统一走 cellValue)
- [ ] 样本/类型/字段变化时快捷值自动重算,不残留旧值
- [ ] 未加载数据/字段值不可抽取时整洁降级(不显示 chips 或显示占位)
- [ ] vue-tsc / eslint 通过;筛选逻辑与组合(AND、keyword)行为不变

## Hard Rules
快捷值仅来自已返回样本(零额外接口请求);不轮询/不预取。

## Implementation Notes
- 复用 `cellValue(row, fieldKey)` 保证与表格列取值一致(归一化字段)。
- 位置:字段筛选行下方一条 metra 区;结构号为 `{field: quickValues}` 的 computed。
- 与 TopN 下钻(任务 6)共用"填充筛选"逻辑时可抽小工具,但任务独立可先各自实现。
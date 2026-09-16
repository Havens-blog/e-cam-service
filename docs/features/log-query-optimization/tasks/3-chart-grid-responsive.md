---
id: "3"
title: "图表栅格/明细区布局自适应(不等宽挤压、表头吸顶)"
priority: "P2"
estimated_time: "1h"
complexity: "low"
dependencies: []
surface-key: ""
surface-type: ""
breaking: false
type: "coding.enhancement"
mainSession: false
---

# 3: 图表栅格/明细区布局自适应(不等宽挤压、表头吸顶)

## Description
统计区「三图+趋势」在窄屏/长分组值下挤压失真(已修 yAxis 标签省略,但栅格宽度仍固定 1fr 易失衡);明细表正文滚动时表头不吸顶。改为:图表栅格按内容/容器自适应最小宽度折行;趋势图全宽、三小图自动等比;明细表容器滚动时表头吸顶、列宽按字段内容给 min-width 保障。不动统计语义。

## Reference Files
- `docs/proposals/log-query-optimization/proposal.md` — Source proposal;«Key Scenarios»
- `e-cam-web/src/views/logs/components/LogStats.vue`: chart-grid 布局
- `e-cam-web/src/views/logs/index.vue`: 明细 el-table/detail-body 滚动区

## Acceptance Criteria
- [ ] 三图+趋势栅格:窄容器下自动折行,三图不等宽且不挤压趋势图;趋势图保持全宽
- [ ] 明细表格纵向滚动时表头吸顶(表头不随行滚出)
- [ ] 窄屏(如 1280px)与宽屏下均无内容被裁切/溢出
- [ ] 长分组值(IPv6/UA)不再导致图表区宽度被撑破(标签早已省略,栅格不再跟随标签宽)
- [ ] vue-tsc / eslint 通过;统计图数据/交互无回归

## Hard Rules
不改统计图的数据口径与图表类型;不引入第三方布局库。

## Implementation Notes
- LogStats.chart-grid 现为固定列;改 `grid-template-columns: repeat(auto-fit, minmax(280px, 1fr))` 类自适应,趋势图 `grid-column: 1 / -1`。
- 明细表头吸顶:el-table 自带表头固定(需容器高度受控时生效),检查 detail-body 容器 overflow 配置;必要时改 `max-height` 为占位+外层滚动。
- 验收在 dev:5173 下用浏览器/devtools 调窄窗口目测。
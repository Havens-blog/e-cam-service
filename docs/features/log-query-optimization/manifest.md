---
feature: "log-query-optimization"
created: "2026-09-16"
status: tasks
mode: quick
---

# Feature (Quick): log-query-optimization

<!-- Status flow: tasks -> in-progress -> completed -->

## Documents

| Document | Path |
|----------|------|
| Proposal | ../../proposals/log-query-optimization/proposal.md |

## Tasks

| ID | Title | Status | File |
|----|-------|--------|------|
| 1 | 定位并修复 sources/search/aggregate 接口延迟(sources 实测 20s) | pending | tasks/1-measure-and-fix-sources-latency.md |
| 2 | 查询进行中状态 + 失败可重试(消除 20s 白屏无回馈) | pending | tasks/2-loading-state-and-retry.md |
| 3 | 图表栅格/明细区布局自适应(不等宽挤压、表头吸顶) | pending | tasks/3-chart-grid-responsive.md |
| 4 | 字段筛选快捷值回填(从已返回样本点选常见值) | pending | tasks/4-filter-quick-values.md |
| 5 | 明细区按云·账号折叠分组展示(组头含条数/耗时) | pending | tasks/5-source-group-fold.md |
| 6 | TopN 分组图点击下钻:点击项自动加字段筛选重查 | pending | tasks/6-topn-drilldown.md |
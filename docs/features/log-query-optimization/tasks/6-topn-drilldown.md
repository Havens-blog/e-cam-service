---
id: "6"
title: "TopN 分组图点击下钻:点击项自动加字段筛选重查"
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

# 6: TopN 分组图点击下钻:点击项自动加字段筛选重查

## Description
分组聚合 TopN 条形图目前只读。改为:点击某分组条 → 自动为该分组值生成字段筛选条件(dimension 字段 = 图当前维度,value = 该项名称,op=eq,若无该字段筛选行则新增),并触发重查;统计图标题/筛选区可一键「清除下钻」还原。若维度为透传原始列(如 real_client_ip),同规则映射到筛选字段。默认维度(域名/规则)下钻到 host/rule_name。

## Reference Files
- `docs/proposals/log-query-optimization/proposal.md` — Source proposal;«Proposed Solution» «Key Scenarios»
- `e-cam-web/src/views/logs/components/LogStats.vue`: topBarOption/thirdOption(echarts bar click)
- `e-cam-web/src/views/logs/index.vue`: fieldFilters/buildFilters/doSearch

## Acceptance Criteria
- [ ] 点击 TopN 条形图任意分组条 → 新增对应字段筛选行(field=维度映射,op=eq,value=项名)并自动重查
- [ ] 维度映射正确:默认维度(域名 Top→host、规则 Top→rule_name)与自定义维度(原始列如 real_client_ip→同名筛选)均成立
- [ ] 若已存在同字段 eq 筛选,更新其值(不重复加行)
- [ ] 筛选区提供「清除下钻」(仅移除下钻加的条件,保留用户原有条件与 keyword)
- [ ] 聚合图(全窗 TopN)下钻后,明细/统计同步按新筛选展示
- [ ] vue-tsc / eslint 通过;非图表区域点击无副作用;无下钻条件时行为与现在一致

## Hard Rules
TS 严格模式(noUncheckedIndexedAccess);不可引入循环依赖(LogStats 通过 emit 而非直接改父级 state)。

## Implementation Notes
- LogStats 增加 `@bar-click` emit(name, dimension),父组件 index.vue 处理「写 fieldFilters + doSearch」。
- 下钻值的 dimension 映射:非空 aggrDimension 直接用;默认维度时 logType=waf→rule_name,否则→host。
- 清除下钻:给下钻条件打标记(can-doing 字段筛选行的 drilldown 标志),「清除下钻」只删带标记行。
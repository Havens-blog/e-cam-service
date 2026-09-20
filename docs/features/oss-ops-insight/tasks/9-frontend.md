---
id: "9"
title: "前端:OSS 抽屉监控 tab 趋势图 + 列表页运营卡(空态/警示/数据来源统一)"
priority: "P1"
estimated_time: "2.5d"
complexity: "high"
dependencies: [8]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 9: 前端:OSS 抽屉监控 tab 趋势图 + 列表页运营卡(空态/警示/数据来源统一)

## Description

OSS 抽屉加「监控」tab(容量柱 + 对象数线双轴趋势,仿 NasDetailDrawer);列表页顶部加运营卡(总容量/对象数/近 7 天增速);数据来源统一 ecam_oss_metric 指标表;新增纯逻辑层 + 单测。

## Reference Files

- `docs/proposals/oss-ops-insight/proposal.md` — Proposed Solution 第 6 条(前端/数据来源声明)、Success Criteria
- `d:\Haven\e-cam-web\src\views\storage\nas\index.vue`: NAS 列表页运营卡先例(数据来源指标表)
- `d:\Haven\e-cam-web\src\views\storage\nas\components\NasDetailDrawer.vue`: NAS 抽屉监控 tab 先例(双轴趋势)
- `d:\Haven\e-cam-web\src\views\storage\nas\nasMetrics.ts`: NAS 纯逻辑层先例(聚合/单位/去重)
- `d:\Haven\e-cam-web\src\views\storage\oss\components\OssDetailDrawer.vue`: OSS 抽屉现状(6 tab 无监控)

## Acceptance Criteria

- [ ] OSS 抽屉「监控」tab:echarts 双轴趋势(容量 GB 柱 + 对象数线),缺失日/zero_exception 断线与警示;数据来自 `/assets/oss/metrics`
- [ ] 列表页运营卡:总容量/对象数/近 7 天增速,经 `/assets/oss/top` 全量汇总;「采集失败→警示」优先「无数据→0 占位」,失败明细读 oss:collect_metrics 任务 Result.failures
- [ ] 数据来源统一:列表行容量/对象数列与导出改由 Top 项取值,抽屉详情移除资产表快照值;OSS 界面一律以指标表为唯一来源
- [ ] 空态区分「无数据」与「采集失败/未启用」;zero_exception 渲染警示而非正常空桶
- [ ] 新增 ossMetrics.ts 纯逻辑层(聚合/单位/去重)+ 单测;前端测试全套绿,vue-tsc/eslint 通过

## Hard Rules

- OSS 界面数据一律来自 ecam_oss_metric 指标表(资产表快照不显示),不得同屏混用

## Implementation Notes

- NAS 前端(nasMetrics.ts/index.vue/NasDetailDrawer.vue)是直接蓝本,OSS 平移。
- 并行会话 WIP(e-cam-web 下 .env.development/element-theme.scss/components.d.ts/accounts/index.vue 等)不得纳入提交——只 stage OSS 相关文件。
- api 层新增 getOssMetricsApi/getOssTopApi(仿 getNasMetricsApi/getNasTopApi)。
- 抽屉「更多」菜单的禁用项(文件管理/基础设置/权限管理)保持既有禁用态,不改。

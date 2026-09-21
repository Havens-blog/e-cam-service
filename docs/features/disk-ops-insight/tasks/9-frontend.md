---
id: "9"
title: "前端:Disk 抽屉监控 tab 趋势图 + 列表页运营卡(空态/警示/数据来源统一)"
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

# 9: 前端:Disk 抽屉监控 tab 趋势图 + 列表页运营卡(空态/警示/数据来源统一)

## Description

Disk 抽屉「监控」tab 填趋势图(使用率折线 + IOPS/吞吐双轴,仿 NasDetailDrawer);列表页顶部加运营卡(总容量/平均使用率/IO 繁忙盘数);数据来源统一 ecam_disk_metric 指标表;新增纯逻辑层 + 单测。

## Reference Files

- `docs/proposals/disk-ops-insight/proposal.md` — Proposed Solution 第 6 条(前端/数据来源声明)、Success Criteria
- `d:\Haven\e-cam-web\src\views\storage\nas\index.vue`: NAS 列表页运营卡先例(数据来源指标表)
- `d:\Haven\e-cam-web\src\views\storage\nas\nasMetrics.ts`: NAS 纯逻辑层先例(聚合/单位/去重)
- `d:\Haven\e-cam-web\src\views\storage\oss\ossMetrics.ts`: OSS 纯逻辑层先例(最近产物,直接蓝本)
- `d:\Haven\e-cam-web\src\views\compute\disk\components\DiskDetailDrawer.vue`: Disk 抽屉现状(监控 tab 空壳待填)

## Acceptance Criteria

- [ ] Disk 抽屉「监控」tab:使用率折线 + IOPS/吞吐双轴趋势(echarts),缺失日/zero_exception 断线与警示;数据来自 `/assets/disk/metrics`
- [ ] 列表页运营卡:总容量/平均使用率/IO 繁忙盘数(使用率 >80% 计数),经 `/assets/disk/top` 全量汇总;「采集失败→警示」优先「无数据→0 占位」,失败明细读 disk:collect_metrics 任务 Result.failures
- [ ] 数据来源统一:列表行容量/使用率/IOPS/吞吐列与导出改由 Top 项取值,抽屉详情移除资产表快照值;Disk 界面一律以指标表为唯一来源
- [ ] 空态区分「无数据」与「采集失败/未启用」;zero_exception 渲染警示而非正常空盘
- [ ] 新增 diskMetrics.ts 纯逻辑层(聚合/单位/去重)+ 单测;前端测试全套绿,vue-tsc/eslint 通过

## Hard Rules

- Disk 界面数据一律来自 ecam_disk_metric 指标表(资产表快照不显示),不得同屏混用

## Implementation Notes

- NAS/OSS 前端(nasMetrics.ts / ossMetrics.ts / index.vue / NasDetailDrawer.vue)是直接蓝本,Disk 平移。
- 并行会话 WIP(e-cam-web 下 .env.development/element-theme.scss/components.d.ts/accounts/index.vue 等)不得纳入提交——只 stage Disk 相关文件。
- api 层新增 getDiskMetricsApi/getDiskTopApi(仿 getNasMetricsApi/getOssMetricsApi)。
- 抽屉「监控」tab 已有空壳 tab 标签,只需填内容分支(勿新增重复 tab)。
- IO 繁忙盘数定义:usage_percent > 80% 或 IOPS 超阈值,与运营卡文案一致,在 diskMetrics.ts 定义。

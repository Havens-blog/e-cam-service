---
id: "9"
title: "前端:RDS 抽屉监控 tab 趋势图 + 列表页运营卡(空态/警示/数据来源统一)"
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

# 9: 前端:RDS 抽屉监控 tab 趋势图 + 列表页运营卡(空态/警示/数据来源统一)

## Description

RDS 抽屉「监控」tab 填趋势图(CPU/内存/磁盘使用率 + 连接数多指标,仿 NasDetailDrawer 多轴);列表页顶部加运营卡(平均 CPU/内存/磁盘水位 + 高负载实例数);数据来源统一 ecam_rds_metric 指标表;新增纯逻辑层 + 单测。

## Reference Files

- `docs/proposals/rds-ops-insight/proposal.md` — Proposed Solution 第 6 条(前端/数据来源声明)、Success Criteria
- `d:\Haven\e-cam-web\src\views\storage\nas\index.vue`: NAS 列表页运营卡先例(数据来源指标表)
- `d:\Haven\e-cam-web\src\views\compute\disk\diskMetrics.ts`: Disk 纯逻辑层先例(最近产物,直接蓝本)
- `d:\Haven\e-cam-web\src\views\compute\disk\index.vue`: Disk 列表页运营卡先例
- `d:\Haven\e-cam-web\src\views\database\rds\components\RdsDetailDrawer.vue`: RDS 抽屉现状(监控 tab 空壳待填)

## Acceptance Criteria

- [ ] RDS 抽屉「监控」tab:CPU/内存/磁盘使用率 + 连接数多指标趋势(echarts),缺失日/zero_exception 断线与警示;数据来自 `/assets/rds/metrics`
- [ ] 列表页运营卡:平均 CPU/内存/磁盘水位 + 高负载实例数(使用率 >80% 计数),经 `/assets/rds/top` 全量汇总;「采集失败→警示」优先「无数据→0 占位」,失败明细读 rds:collect_metrics 任务 Result.failures
- [ ] 数据来源统一:列表行容量/负载列与导出改由 Top 项取值,抽屉详情移除资产表快照值;RDS 界面一律以指标表为唯一来源
- [ ] 空态区分「无数据」与「采集失败/未启用」;zero_exception 渲染警示而非正常空负载;停用中实例打标展示
- [ ] 新增 rdsMetrics.ts 纯逻辑层(聚合/单位/去重)+ 单测;前端测试全套绿,vue-tsc/eslint 通过

## Hard Rules

- RDS 界面数据一律来自 ecam_rds_metric 指标表(资产表快照不显示),不得同屏混用

## Implementation Notes

- Disk 前端(diskMetrics.ts/index.vue/DiskDetailDrawer.vue)是直接蓝本,RDS 平移;RDS 是 database 域(与 compute/storage 域路径不同,确认 RdsDetailDrawer 所在目录)。
- 并行会话 WIP(e-cam-web 下 .env.development/element-theme.scss/components.d.ts/accounts/index.vue 等)不得纳入提交——只 stage RDS 相关文件。
- api 层新增 getRdsMetricsApi/getRdsTopApi(仿 getDiskMetricsApi/getDiskTopApi)。
- 抽屉「监控」tab 已有空壳 tab 标签,只需填内容分支(勿新增重复 tab)。
- 高负载实例数定义:任一使用率 >80%,与运营卡文案一致,在 rdsMetrics.ts 定义。

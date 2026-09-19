---
id: "11"
title: "前端:NAS 抽屉监控 tab 趋势图 + 列表页运营卡(空态/警示/数据来源统一)"
priority: "P1"
estimated_time: "2.5d"
complexity: "medium"
dependencies: [10]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 11: 前端:NAS 抽屉监控 tab 趋势图 + 列表页运营卡(空态/警示/数据来源统一)

## Description

NAS 抽屉「监控」tab 填入容量/已用/使用率趋势图(echarts),NAS 列表页顶部加运营卡(总容量/已用/平均使用率)。空态区分「无数据」与「采集失败/未启用」,采集异常显示警示;NAS 界面数据来源统一为指标表(资产表坏值不再上屏)。

## Reference Files
- `docs/proposals/nas-ops-insight/proposal.md` — Proposed Solution、失败可观测性、聚合口径、Success Criteria
- `e-cam-web/src/views/network/cdn/components/CdnDetailDrawer.vue`: CDN 指标趋势 tab 参照(echarts 双轴 + 空态)
- `e-cam-web/src/views/network/cdn/index.vue`: CDN 近2日流量卡参照(运营卡模式)
- `e-cam-web/src/views/storage/nas/components/NasDetailDrawer.vue`: NAS 抽屉(现 monitor tab 为空壳,41-47 行 tabs 声明)
- `e-cam-web/src/views/storage/nas/index.vue`: NAS 列表页(现顶部统计徽章,7-19 行)
- `e-cam-web/src/api/asset.ts`: NAS 指标 API 定义(新增 getNasMetricsApi/getNasTopApi)

## Acceptance Criteria
- [ ] NAS 抽屉「监控」tab 填入容量/已用/使用率趋势图(echarts,仿 CdnDetailDrawer 双轴/connectNulls);有数据渲染、无数据显示空态(不白屏不报错)
- [ ] 列表页顶部运营卡:总容量/已用容量/平均使用率(数据来源 `ecam_nas_metric` 指标表,经 /assets/nas/top)
- [ ] 空态区分「无数据(指标真实为 0/空)」与「采集失败/未启用」;运营卡判定顺序「采集失败→警示」优先于「无数据→0 占位」(以任务 Result 失败计数为准)
- [ ] qc_status 展示:capacity=0(zero_exception)渲染异常标记,不当正常零容量
- [ ] NAS 界面数据来源统一:列表行/运营卡/趋势/Top 均以指标表为准;资产表 capacity/used 不再在 NAS 界面展示
- [ ] 趋势 tab 下拉时按需 fetch(fs_id + account_id + days),切换实例重置;typecheck 通过

## Hard Rules
- NAS 界面容量数值一律来自指标表(不得展示资产表坏值)
- 采集失败显示警示,不得把未知伪装成 0 占位

## Implementation Notes
- 参照 `CdnDetailDrawer.vue` 的指标 tab:metricsLoading/metricsError/metricsItems + echarts 双轴(柱=容量/已用,线=使用率;-1/空 → null + connectNulls:false)。
- 参照 `network/cdn/index.vue` 的近2日流量卡(统计分析卡模式)放 NAS 运营卡。
- 空态判定以执行器任务 Result 失败计数为准:该账号/厂商全部实例失败 → 警示;任务成功且指标真实 0/空 → 0 占位。
- 趋势图「近 N 天峰值」= 日值最大值(读取接口 MAX 派生),前端不承诺日内尖峰。
- 注意 NAS 抽屉现有 monitor tab 是空壳(无内容分支),本次填入;log tab 保持现状。

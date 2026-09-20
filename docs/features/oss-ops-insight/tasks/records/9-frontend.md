---
status: "completed"
started: "2026-09-20 13:19"
completed: "2026-09-20 13:28"
time_spent: "~9m"
---

# Task Record: 9 前端:OSS 抽屉监控 tab 趋势图 + 列表页运营卡(空态/警示/数据来源统一)

## Summary
OSS 前端经营洞察:抽屉新增「监控」tab(echarts 双轴:存储量 GB 柱 + 对象数线,缺失日断线,zero_exception 警示,数据来自 /assets/oss/metrics);列表页新增运营卡(总容量/对象数量/近 7 天增速/存储桶数,经 /assets/oss/top 翻页全量汇总,采集失败警示优先 0 占位,失败明细读 oss:collect_metrics 任务 Result.failures);数据来源统一指标表:列表行存储量/对象数列与导出改由 Top 项(bucket_name=asset_id)取值,抽屉详情移除资产表快照值;新增 ossMetrics.ts 纯逻辑层(聚合/增速/卡态判定/失败提取/zero_exception/单位格式化,formatCapacityGB 复用 NAS 口径) + 18 单测。api 层新增 getOssMetricsApi/getOssTopApi 与类型(仿 NAS)。

## Changes

### Files Created
- e-cam-web/src/views/storage/oss/ossMetrics.ts
- e-cam-web/src/views/storage/oss/ossMetrics.test.ts

### Files Modified
- e-cam-web/src/api/asset.ts
- e-cam-web/src/views/storage/oss/index.vue
- e-cam-web/src/views/storage/oss/components/OssDetailDrawer.vue

### Key Decisions
- 近 7 天增速口径:合计最新容量 vs 合计近 7 天均值(days=8,当日行仅含凌晨部分),比率口径合计后再相比不逐桶平均
- formatCapacityGB 直接复用 nas/nasMetrics 单一实现(经 ossMetrics re-export),避免单位口径漂移
- 运营卡/列/导出全量 Top 拉取按 page_size=50 翻页上限 20 页,与 NAS 同构
- 抽屉详情快照行移除后留一行提示「见监控 tab」,保证 Hard Rule(不显示资产表快照)同时不丢入口引导
- 并行会话 WIP 文件(.env.development/element-theme.scss/components.d.ts/accounts/index.vue)未纳入提交,仅 stage OSS 相关文件

## Test Results
- **Tests Executed**: Yes
- **Passed**: 530
- **Failed**: 0
- **Coverage**: 0.0%

## Acceptance Criteria
- [x] OSS 抽屉「监控」tab:echarts 双轴趋势(容量 GB 柱 + 对象数线),缺失日/zero_exception 断线与警示;数据来自 /assets/oss/metrics
- [x] 列表页运营卡:总容量/对象数/近 7 天增速,经 /assets/oss/top 全量汇总;「采集失败→警示」优先「无数据→0 占位」,失败明细读 oss:collect_metrics 任务 Result.failures
- [x] 数据来源统一:列表行容量/对象数列与导出改由 Top 项取值,抽屉详情移除资产表快照值
- [x] 空态区分「无数据」与「采集失败/未启用」;zero_exception 渲染警示而非正常空桶
- [x] 新增 ossMetrics.ts 纯逻辑层(聚合/单位/去重)+ 单测;前端测试全套绿,vue-tsc/eslint 通过

## Notes
全套 vitest 530/530 绿(含新增 18 用例);ossMetrics.ts 全部导出函数均有直接用例覆盖;coverage 字段省略:e-cam-web 未安装 @vitest/coverage-v8,runner 无法产出真实覆盖率,按规则不猜测。vue-tsc 仅 src/views/logs 测试 2 个既有错误(他人会话,非本任务文件);eslint 对改动文件通过。工作在 e-cam-web 仓库。

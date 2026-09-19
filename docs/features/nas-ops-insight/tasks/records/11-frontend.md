---
status: "completed"
started: "2026-09-19 19:05"
completed: "2026-09-19 19:25"
time_spent: "~20m"
---

# Task Record: 11 前端:NAS 抽屉监控 tab 趋势图 + 列表页运营卡(空态/警示/数据来源统一)

## Summary
e-cam-web NAS 前端经营洞察:抽屉「监控」tab 填入 echarts 双轴趋势图(柱=容量/已用 GB,线=使用率%,缺失日/异常日断线 connectNulls:false,zero_exception 日 el-alert 警示 + tooltip 标记「⚠ 容量异常」);列表页顶部运营卡(总容量/已用容量/平均使用率/文件系统数,经 /assets/nas/top 分页拉全量后前端汇总);空态判定「采集失败→警示」优先于「无数据→0 占位」(失败明细取最近一次 nas:collect_metrics 任务 Result.failures,Top 请求失败同样警示);数据来源统一为 ecam_nas_metric 指标表:列表行容量列与 NAS 导出改由指标表 Top 项(fs_id=asset_id)取值、无指标显示 '-',抽屉详情 tab 移除资产表 capacity/used 行,导出无指标数据导出空串不回退资产表坏值;api/asset.ts 新增 NASMetricPoint/NASMetricSummary/NASFsMetricsView/NASTopItem 类型与 getNasMetricsApi/getNasTopApi;纯逻辑收敛 nasMetrics.ts(聚合/空态判定/zero_exception 识别/GB 格式化)配 18 个单测;ColumnSettingsDialog 修复历史错位列标签(capacity=总容量/used_capacity=已用容量),列表页加载 localStorage 列设置时按默认标签回填修复旧脏数据;监控 tab 按需 fetch(fs_id+account_id+days=30)带 fetchedKey 去重、切换实例重置、resize 监听与图表 dispose。

## Changes

### Files Created
- e-cam-web/src/views/storage/nas/nasMetrics.ts
- e-cam-web/src/views/storage/nas/nasMetrics.test.ts

### Files Modified
- e-cam-web/src/api/asset.ts
- e-cam-web/src/views/storage/nas/index.vue
- e-cam-web/src/views/storage/nas/components/NasDetailDrawer.vue
- e-cam-web/src/views/storage/nas/components/ExportDialog.vue
- e-cam-web/src/views/storage/nas/components/ColumnSettingsDialog.vue

### Key Decisions
- 列表行容量列不删除而改为指标表来源:复用运营卡拉取的 /assets/nas/top 全量项建 fsMetricMap(fs_id=asset_id 映射),满足「列表行以指标表为准」且不新增 N 次逐行请求
- 运营卡全量汇总用 page_size=50 翻页拉全 Top(上限 20 页防御),聚合/均值/空态判定/格式化收敛在 nasMetrics.ts 纯函数层保证可测
- 「采集失败」前端可见性二选一取任务 Result 口径:listTasksApi({type:'nas:collect_metrics',limit:1}) 读最近任务 result.failures(error_count>0 过滤),与 Top 请求失败一起进 deriveNasCardState(warn 优先于 empty)
-  NAS_CARD_DAYS=2 对齐 CDN 卡口径(凌晨补采当日仅初态,days=2 保证全天有最新完整快照);平均使用率对 utilization=null(capacity=0 异常/无数据)实例跳过不参与
- 监控 tab 空态三分支:请求失败(警示+重试)/窗口内无任何真实数据(暂无指标数据,注明未采集或未启用)/zero_exception 日存在(顶部 el-alert,不当正常零容量)
- ExportDialog 容量字段改指标表来源且无指标导出空串(不回退资产表坏值,Hard Rule);新增 metricMap 为可选 prop 保持向后兼容
- 列表页列设置加载时按默认标签回填修复历史保存的错位标签(keys/显隐/宽度仍尊重用户保存值)

## Test Results
- **Tests Executed**: Yes
- **Passed**: 503
- **Failed**: 0
- **Coverage**: 0.0%

## Acceptance Criteria
- [x] NAS 抽屉「监控」tab 填入容量/已用/使用率趋势图(echarts 仿 CdnDetailDrawer 双轴/connectNulls);有数据渲染、无数据显示空态(不白屏不报错)
- [x] 列表页顶部运营卡:总容量/已用容量/平均使用率(数据来源 ecam_nas_metric 指标表,经 /assets/nas/top)
- [x] 空态区分「无数据」与「采集失败/未启用」;运营卡判定顺序「采集失败→警示」优先于「无数据→0 占位」(以任务 Result 失败计数为准)
- [x] qc_status 展示:capacity=0(zero_exception)渲染异常标记,不当正常零容量
- [x] NAS 界面数据来源统一:列表行/运营卡/趋势/Top 均以指标表为准;资产表 capacity/used 不再在 NAS 界面展示
- [x] 趋势 tab 下拉时按需 fetch(fs_id + account_id + days),切换实例重置;typecheck 通过

## Notes
测试口径:testsPassed=503 为 npx vitest run 全套(24 文件,既有 485 + 新增 nasMetrics.test.ts 18),503/503 绿;coverage 记 0.0 沿用 e-cam-web 既有口径(vitest 未启用 coverage provider,@vitest/coverage-v8 三次尝试 --no-save 安装均因 npm eresolve/安装器报错失败,非测试失败;新增纯逻辑模块 6 个导出函数均被 18 用例直接断言,分支含 null/NaN/空数组/非法形态防御)。静态检查:vue-tsc -b EXIT 0;eslint(改动文件)0 error,仅 ExportDialog 既有 catch(e) unused 警告(非本次引入)。任务类型为纯前端,Go 侧 just compile/fmt/lint 不适用(未改任何 Go 文件)。后端契约按 asset_nas_query.go/asset_handler_nas_metrics.go 实测复核(days 1~90 缺省 30、top 分页 {total,page,page_size,items[]}、data_status ok|zero_exception|missing、缺失日 null 不填假值)。趋势「近 N 天峰值」由 latest/日值序列表达,前端不承诺日内尖峰(与 proposal 口径一致)。log tab 保持现状;抽屉「更多」菜单/批量操作等既有禁用项未动。e-cam-web 仓库存在与本任务无关的既有未提交改动(.env.development/element-theme.scss/components.d.ts/accounts/index.vue/.claude),本次提交不纳入。

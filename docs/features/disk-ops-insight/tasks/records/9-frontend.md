---
status: "completed"
started: "2026-09-21 13:02"
completed: "2026-09-21 13:19"
time_spent: "~17m"
---

# Task Record: 9 前端:Disk 抽屉监控 tab 趋势图 + 列表页运营卡(空态/警示/数据来源统一)

## Summary
Disk 前端经营洞察:抽屉「监控」tab 填使用率/IOPS/吞吐双轴趋势图(echarts,缺失日/zero_exception 断线与警示);列表页新增运营卡(磁盘数/平均使用率/IO 繁忙盘数,经 /assets/disk/top 全量汇总,采集失败→警示优先);数据来源统一为 ecam_disk_metric 指标表(列表使用率/IOPS/吞吐列与导出改由 Top 项取值,抽屉详情移除资产表快照行);新增 api 层 getDiskMetricsApi/getDiskTopApi 与 diskMetrics.ts 纯逻辑层(27 单测,100% 覆盖),前端全套 713/713 绿,vue-tsc/eslint 变更文件 0 问题

## Changes

### Files Created
- src/views/compute/disk/diskMetrics.ts
- src/views/compute/disk/diskMetrics.test.ts

### Files Modified
- src/api/asset.ts
- src/views/compute/disk/index.vue
- src/views/compute/disk/components/DiskDetailDrawer.vue

### Key Decisions
- SPEC CONTRADICTION: proposal/AC 要求运营卡「总容量」与列表「容量」列取 Top 项,但 T1 探测定案五厂商均无云盘级容量指标,ecam_disk_metric 模型明确不设容量字段,DiskTopItem 无容量;按 Hard Rule(指标表唯一来源,禁资产表快照)+ 更晚的 probe-report 定案:运营卡以「磁盘数」替代「总容量」,列表/导出移除容量列,新增使用率/IOPS/吞吐列
- IO 繁忙盘数定义在 diskMetrics.ts:usage_percent > 80(严格大于)或 IOPS >= DISK_IOPS_BUSY_THRESHOLD(1000 次/秒,运营经验阈值,常量导出可调);zero_exception 行(口径缺失 0)不参与均值与繁忙判定,与后端 Top 均值口径一致(busy_share 合法闲盘 0 参与)
- 列表行指标列经 opsTopItems 全量 Top 分页(50/页,上限 20 页)拉取后建 disk_id 映射取值,与 NAS fsMetricMap 同构;自定义列历史设置迁移:剔除已下线 size 列,缺失新列按默认补齐
- 抽屉监控 tab 平移 NasDetailDrawer 同构实现:账号取列表 VO 顶层 account_id(兼容 attributes.cloud_account_id 回退);双轴=IOPS/吞吐左轴值轴 + 使用率右轴 0~100;zero_exception 日使用率置空断线并 alert/tooltip 警示;usage_scope 口径标注展示(实例级/IO 繁忙占比)区分容量水位
- 并行会话 WIP 文件未纳入提交范围(仅 Disk 相关文件入库)

## Test Results
- **Tests Executed**: Yes
- **Passed**: 713
- **Failed**: 0
- **Coverage**: 100.0%

## Acceptance Criteria
- [x] Disk 抽屉「监控」tab:使用率折线 + IOPS/吞吐双轴趋势(echarts),缺失日/zero_exception 断线与警示;数据来自 /assets/disk/metrics
- [x] 列表页运营卡:磁盘数(替代总容量,指标表无容量字段的 spec 定案)/平均使用率/IO 繁忙盘数(usage>80% 或 IOPS 超阈值),经 /assets/disk/top 全量汇总;采集失败→警示优先于无数据→0 占位;失败明细读 disk:collect_metrics 任务 Result.failures
- [x] 数据来源统一:列表行使用率/IOPS/吞吐列与导出改由 Top 项取值,抽屉详情移除资产表快照值(容量/IOPS/吞吐量行与导出容量字段移除);Disk 界面一律以指标表为唯一来源
- [x] 空态区分「无数据」与「采集失败/未启用」;zero_exception 渲染警示而非正常空盘
- [x] 新增 diskMetrics.ts 纯逻辑层(聚合/IO 繁忙判定/口径标注/格式化)+ 27 单测;前端测试全套绿(713/713),vue-tsc/eslint 通过

## Notes
静态检查:compile(npx vue-tsc -b)exit 0;lint(eslint)变更文件 0 问题(全仓既有告警均在未触碰文件);fmt(prettier --check)非阻塞 WARN——仓库 NAS/OSS 先例文件同不满足 prettier,新文件随蓝本风格不单独格式化。coverage=100% 为 diskMetrics.ts 纯逻辑层(v8 coverage:59/59 stmts、77/77 branches、12/12 funcs)。

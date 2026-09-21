---
status: "completed"
started: "2026-09-21 10:09"
completed: "2026-09-21 10:53"
time_spent: "~44m"
---

# Task Record: 1 Disk 厂商监控 API 探测:namespace/指标名实盘验证 + 使用率口径归一 + 分布定分组

## Summary
M1 Disk 厂商监控 API 探测完成:5 厂商 namespace/指标名实盘验证 + 使用率口径归一 + 实盘分布定分组,产出发布 gate 探测报告 docs/features/disk-ops-insight/probe-report.md。定案:必达=aliyun(acs_ecs_dashboard,IOPS/吞吐云盘级 diskId)/huawei(SYS.EVS disk_device_* 系列+SYS.ECS disk_util_inband)/aws(AWS/EBS 五指标+VolumeIdleTime 派生公式);tencent 探测未通过(QCE/CBS 仅注册快照类 2 指标+数据权限未开通)且占比 0.49%≤15% 维持尽力而为;volcengine 占比 50.96%>15% 但订阅未开通→二期补。使用率口径核心定案:五厂商均无云盘级容量使用率,须打标 instance_level/busy_share。实盘分布:3295 盘/1,024,937 GB(aliyun 40.00%/43.18%,volcano 50.96%/46.33%,huawei 4.83%/4.12%,aws 3.73%/6.02%,tencent 0.49%/0.36%)

## Changes

### Files Created
- internal/shared/cloudx/nasprobe/disk_usage.go
- internal/shared/cloudx/nasprobe/disk_usage_test.go
- internal/shared/cloudx/nasprobe/disk_distribution_manual_test.go
- internal/shared/cloudx/aliyun/disk_probe_manual_test.go
- internal/shared/cloudx/huawei/disk_probe_manual_test.go
- internal/shared/cloudx/aws/disk_probe_manual_test.go
- internal/shared/cloudx/tencent/disk_probe_manual_test.go
- internal/shared/cloudx/volcano/disk_probe_manual_test.go
- docs/features/disk-ops-insight/probe-report.md

### Files Modified
无

### Key Decisions
- 使用率口径定案(AC-5):五厂商均无云盘级容量使用率直接指标——aliyun/huawei 只有挂载实例/设备级百分比,AWS 派生公式语义为繁忙时间占比;usage_percent 需新增口径标注(instance_level/busy_share),厂商间数值不可直接横比
- AWS 派生公式 usage%=(1−VolumeIdleTime/86400)×100 实盘成立(5/5 PASS)但为繁忙占比非容量水位,本期 AWS 使用率打标 busy_share
- 华为 SYS.EVS disk_device_* 实盘维度键=实例UUID+设备名(非卷 ID/用户命名),T3/T4 须前缀匹配
- 阿里 DiskReadWrite*BurstUtilization 实盘含 -1 哨兵值,T3 采集边界须过滤
- 分组定案(发布 gate):必达=aliyun/huawei/aws;volcengine 占比>15% 但探测不可用→二期补(发布说明承诺补采窗口);tencent 占比≤15% 且探测未通过→维持尽力而为
- 复用 nasprobe 通用分布工具(Verdict/AggregateDistribution/LoadProbeAccounts),新增 Disk 语义聚合 DisksToUsage 与派生公式 DiskUsagePercentFromIdle
- 全程只读(Hard Rule):仅 List/DescribeMetric*/GetMetricData/ShowMetricData 类只读 API,凭证内存解密日志掩码,未触碰生产表

## Test Results
- **Tests Executed**: Yes
- **Passed**: 11
- **Failed**: 0
- **Coverage**: 47.4%

## Acceptance Criteria
- [x] 华为:云硬盘使用率/IOPS/吞吐指标实盘验证(候选 namespace SYS.EVS/SYS.ECS 经探测确认),≥1 个真实磁盘非零数据点通过;口径以实盘为准记录
- [x] AWS:EBS VolumeReadBytes/VolumeWriteBytes/VolumeIdleTime 实盘非零验证;确认磁盘使用率派生公式(从 VolumeIdleTime)并记录其繁忙占比语义
- [x] aliyun:云盘使用率/IOPS/吞吐(acs_ecs_dashboard 磁盘指标)实盘非零验证
- [x] tencent/volcengine:探测并归因(指标订阅未开通 vs 指标不存在),记录二期补路径
- [x] 使用率口径确认:各厂商云盘级 vs 挂载实例级归一口径(0~100);无直接使用率厂商记录派生方案
- [x] 分布统计与报告:实盘 Disk 数/容量双口径占比、升格/降级判定、probe-report.md 产出(namespace 定案/口径/非零验证/分组决策/降级预案)

## Notes
coverage 47.4% 为 nasprobe 包整体实测值(gofmt -cover),本次新增 disk_usage.go 三函数 100% 覆盖;探测日志在 logs/disk_probe_*.txt(gitignored);live 探测含 aws region 扩展(NAS_PROBE_AWS_REGIONS,账号 regions 配置与实盘不一致,已登记遗留行动);探针为 env 门控 manual test(无 NAS_PROBE_MONGODB_DSN 自动 skip,SKIP gate 按设计不跑);go vet ./internal/shared/cloudx/... 全绿,-race 因宿主无 cgo 未启用(既有约束)

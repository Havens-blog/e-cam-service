---
id: "1"
title: "Disk 厂商监控 API 探测:namespace/指标名实盘验证 + 使用率口径归一 + 分布定分组"
priority: "P0"
estimated_time: "1d"
complexity: "high"
dependencies: []
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 1: Disk 厂商监控 API 探测:namespace/指标名实盘验证 + 使用率口径归一 + 分布定分组

## Description

探测 5 厂商(aliyun/huawei/aws/tencent/volcengine)云硬盘监控 API 的 namespace/指标名,用真实账号实盘验证使用率/IOPS/吞吐指标非零;确认磁盘使用率口径(云盘级 vs 挂载实例级,AWS 无直接使用率需确认派生);统计实盘 Disk 分布定「必达 vs 尽力而为」分组——发布 gate,后续适配器任务(T3/T4)依据。

## Reference Files

- `docs/proposals/disk-ops-insight/proposal.md` — Proposed Solution(必达厂商选择依据/使用率口径)、Requirements Analysis、Key Risks、Success Criteria
- `docs/features/oss-ops-insight/probe-report.md`: OSS 探测报告先例(namespace 定案/非零验证/分组决策)
- `internal/shared/cloudx/nasprobe/probe.go`: 探测共享包(单位换算/数量级自检/分布聚合/升格判定)
- `internal/shared/cloudx/nasprobe/accounts.go`: 只读账号加载器
- `internal/shared/cloudx/nasprobe/oss_usage.go`: OSS 探测纯逻辑先例

## Acceptance Criteria

- [ ] 华为:云硬盘使用率/IOPS/吞吐指标实盘验证(候选 namespace 如 `SYS.ECS` 或云硬盘专属,经探测确认),≥1 个真实磁盘非零数据点通过;文档口径与实盘不符以实盘为准并记录(参照 NAS SYS.EFS 定案)
- [ ] AWS:EBS `VolumeReadBytes`/`VolumeWriteBytes`/`VolumeIdleTime` 实盘非零验证;确认磁盘使用率派生公式(从 VolumeIdleTime)或记录「无直接使用率」处理方案
- [ ] aliyun:云盘使用率/IOPS/吞吐(`acs_ecs_dashboard` 磁盘指标)实盘非零验证
- [ ] tencent/volcengine:探测并归因(指标订阅未开通 vs 指标不存在),记录二期补路径(参照 NAS/OSS volcengine 先例)
- [ ] 使用率口径确认:各厂商使用率是云盘级还是挂载实例级,归一口径(百分比 0~100);无直接使用率的厂商记录派生方案
- [ ] 分布统计与报告:实盘 Disk 数/容量按厂商占比(双口径)判定升格/降级(占比 >15% 且探测可用→升格必达;否则维持尽力而为并记录理由),产出探测报告 `docs/features/disk-ops-insight/probe-report.md`(namespace 定案/使用率口径/非零验证/分组决策/降级预案),作为 T3/T4 依据

## Hard Rules

- 全程只读:探测只用真实账号只读 API,不写任何生产数据
- 必达/尽力而为分组必须由探测实盘证据支撑,不得凭实现便利臆断

## Implementation Notes

- 复用 `nasprobe` 的只读加载器与单位/分布工具,不重复造;Disk 专属 namespace 探测新增到对应厂商包(env 门控 manual probe 测试,参照 NAS/OSS `probe_manual_test.go` 模式)。
- AWS 使用率派生(VolumeIdleTime)是本任务关键决策点——派生公式需探测验证;不可靠则记录「本期只采 IOPS/吞吐,使用率打标缺失」。
- volcengine 若有指标订阅未开通问题,固化重试路径到探测脚本注释(参照 NAS probe §1.4)。
- 报告为发布 gate,未出报告不得进入 T3/T4。

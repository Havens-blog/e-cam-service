---
id: "1"
title: "RDS 厂商监控 API 探测:namespace/指标名实盘验证 + 内存口径归一 + 多引擎确认 + 分布定分组"
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

# 1: RDS 厂商监控 API 探测:namespace/指标名实盘验证 + 内存口径归一 + 多引擎确认 + 分布定分组

## Description

探测 5 厂商(aliyun/huawei/aws/tencent/volcengine)云数据库 RDS 监控 API 的 namespace/指标名,用真实账号实盘验证 CPU/内存/磁盘/连接数指标非零;确认内存使用率口径(阿里直接给 vs AWS FreeableMemory 换算);确认多引擎(mysql/pg/mariadb/sqlserver)指标口径一致性;统计实盘 RDS 分布定「必达 vs 尽力而为」分组——发布 gate,后续适配器任务(T3/T4)依据。

## Reference Files

- `docs/proposals/rds-ops-insight/proposal.md` — Proposed Solution(必达厂商选择依据/内存口径/多引擎)、Requirements Analysis、Key Risks、Success Criteria
- `docs/features/disk-ops-insight/probe-report.md`: Disk 探测报告先例(namespace 定案/口径归一/分组决策)
- `internal/shared/cloudx/nasprobe/probe.go`: 探测共享包(单位换算/数量级自检/分布聚合/升格判定)
- `internal/shared/cloudx/nasprobe/accounts.go`: 只读账号加载器
- `internal/shared/cloudx/types/rds.go`: RDSInstance 结构(Engine/CPU/Memory/Storage 字段)

## Acceptance Criteria

- [ ] 华为:云数据库 CPU/内存/磁盘/连接数指标实盘验证(候选 namespace `SYS.RDS`),≥1 个真实实例非零数据点通过;文档口径与实盘不符以实盘为准并记录(参照 NAS SYS.EFS 定案)
- [ ] AWS:RDS `CPUUtilization`/`FreeableMemory`/`FreeStorageSpace`/`DatabaseConnections` 实盘非零验证;确认内存使用率换算公式(从 FreeableMemory)
- [ ] aliyun:云数据库 CPU/内存/磁盘/连接数(`acs_rds_dashboard`)实盘非零验证
- [ ] tencent/volcengine:探测并归因(指标订阅未开通 vs 指标不存在),记录二期补路径(参照 NAS/OSS/Disk volcengine 先例)
- [ ] 口径确认:各厂商内存使用率是直接给还是需换算(阿里直接/AWS FreeableMemory),多引擎(mysql/pg/mariadb/sqlserver)指标口径是否一致(差异大则适配器按 engine 分派),归一口径(百分比 0~100)
- [ ] 分布统计与报告:实盘 RDS 数/规格按厂商占比(双口径)判定升格/降级(占比 >15% 且探测可用→升格必达;否则维持尽力而为并记录理由),产出探测报告 `docs/features/rds-ops-insight/probe-report.md`(namespace 定案/内存口径/多引擎/非零验证/分组决策/降级预案),作为 T3/T4 依据

## Hard Rules

- 全程只读:探测只用真实账号只读 API,不写任何生产数据
- 必达/尽力而为分组必须由探测实盘证据支撑,不得凭实现便利臆断

## Implementation Notes

- 复用 `nasprobe` 的只读加载器与单位/分布工具,不重复造;RDS 专属 namespace 探测新增到对应厂商包(env 门控 manual probe 测试,参照 NAS/OSS/Disk `probe_manual_test.go` 模式)。
- 内存换算(FreeableMemory)是本任务关键决策点——换算公式需探测验证;不可靠则记录「本期内存使用率打标缺失」。
- volcengine 若有指标订阅未开通问题,固化重试路径到探测脚本注释(参照 NAS probe §1.4)。
- 报告为发布 gate,未出报告不得进入 T3/T4。

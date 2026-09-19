---
id: "1"
title: "厂商监控 API 探测:volcengine 指标名 / 华为 namespace / 实盘非零验证 / 容量分布统计"
priority: "P0"
estimated_time: "2d"
complexity: "high"
dependencies: []
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 1: 厂商监控 API 探测:volcengine 指标名 / 华为 namespace / 实盘非零验证 / 容量分布统计

## Description

NAS 指标采集的「必达 vs 尽力而为」分组是发布 gate(M1)。在实现适配器之前,用真实账号各跑一次监控 API 探测,固定各厂商指标名/namespace,验证实盘 NAS 实例监控 API 返回非零数据,并统计实盘容量按厂商分布作为分组证据。

## Reference Files
- `docs/proposals/nas-ops-insight/proposal.md` — 单位归一化与字段语义、Constraints & Dependencies、Key Risks、Success Criteria、Next Steps
- `internal/shared/cloudx/volcano/nas.go`: 现有火山 NAS 资源适配器,探测 client 复用其凭证/region 模式
- `internal/shared/cloudx/huawei/sfs.go`: 现有华为 SFS 资源适配器,探测 CES 需新建 client
- `internal/shared/cloudx/aws/efs.go`: 现有 AWS EFS 适配器,CloudWatch client 需新增
- `internal/shared/cloudx/aliyun/nas.go`: 阿里 NAS 适配器,CMS client 模式参照

## Acceptance Criteria
- [ ] volcengine cloudmonitor 探测:用真实账号跑一次 GetMetricData,确认 NAS 容量/使用量指标名;成功=写样例行并固定指标名,失败=记录错误+候选指标名+「二期补」明确判定
- [ ] 华为 CES 探测:按文件系统类型分别跑 `SYS.SFS`(普通 SFS)与 `SYS.SFS_Turbo`(Turbo)两个 namespace,确认容量指标可用;探测失败按「华为降级为尽力而为、SC-1 改写为 aliyun/aws 必达」处置并写入探测报告
- [ ] 华为/AWS 实盘非零验证:对实盘这批 capacity=0 的 NAS 实例,确认监控 API(华为 CES / AWS CloudWatch `StorageBytes`)返回非零且数量级正确;零值视为探测未通过并记录原因
- [ ] 实盘容量按厂商分布统计:输出各厂商 NAS 实例数/容量占比,作为必达 vs 尽力而为分组与覆盖承诺的依据;若 tencent/volcengine 任一占比 >15% 且探测可用,标记为升格为必达项候选
- [ ] 产出 `M1 探测报告`(路径 `docs/features/nas-ops-insight/probe-report.md`):各厂商指标名/namespace 定案、非零验证结果、容量分布、必达/尽力而为最终分组 + 升格判定

## Hard Rules
- 探测用只读凭证,只写样例行到临时 collection/日志,不动生产 `ecam_nas_metric` 表
- 华为 namespace 探测必须 SFS 与 SFS_Turbo 分开,不得一刀切
- 不动并行会话 WIP;显式文件提交

## Implementation Notes
- 探测是发布 gate:T1 未定案,后续 T3 适配器的必达/尽力而为分组悬空;探测结果须落到 `probe-report.md` 供 T3 引用。
- 若探测脚本以一次性 Go test 形式编写(参照 CDN 华为指标修复时 `TestManualProbe*` 模式),运行后保留为 `internal/shared/cloudx/<provider>/probe_manual_test.go`(带 SKIP gate,无 env 不跑)。
- 参考阿里 CMS `DescribeMetricList`、AWS EFS `StorageBytes`(https://docs.aws.amazon.com/efs/latest/ug/monitoring-cloudwatch.html)、华为 CES `BatchListMetricData`(https://support.huaweicloud.com/api-ces/index.html)。
- 风险提示(proposal Key Risks):volcengine 监控文档稀缺是最大外部不确定;华为探测失败已有条件式达标预案,不阻塞 aliyun/aws 上线。

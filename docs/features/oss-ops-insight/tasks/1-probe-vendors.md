---
id: "1"
title: "OSS 厂商监控 API 探测:namespace/指标名实盘验证 + 分布统计定分组"
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

# 1: OSS 厂商监控 API 探测:namespace/指标名实盘验证 + 分布统计定分组

## Description

探测 5 厂商(aliyun/huawei/aws/tencent/volcengine)对象存储监控 API 的 namespace/指标名,用真实账号实盘验证容量/对象数指标非零;统计实盘 OSS bucket 分布(按厂商 bucket 数/容量占比),定「必达 vs 尽力而为」分组——这是发布 gate,后续适配器任务(T3/T4)的依据。

## Reference Files

- `docs/proposals/oss-ops-insight/proposal.md` — Proposed Solution(必达厂商选择依据)、Requirements Analysis、Key Risks、Success Criteria
- `internal/shared/cloudx/nasprobe/probe.go`: NAS 探测共享包(单位换算/数量级自检/分布聚合/升格判定),OSS 探测可参照或复用其通用逻辑
- `internal/shared/cloudx/nasprobe/accounts.go`: 只读账号加载器(NAS 探测用,OSS 探测同样只读)
- `internal/shared/cloudx/huawei/nas_metrics.go`: 华为 CES `SYS.EFS` 探测先例(文档口径 vs 实盘口径差异的处理方式)
- `internal/shared/cloudx/aliyun/nas_metrics.go`: 阿里 CMS `acs_nas` 探测先例(namespace 归一化)

## Acceptance Criteria

- [ ] 华为:OBS bucket 级容量/对象数指标实盘验证(候选 namespace `SYS.OBS`),≥1 个真实 bucket 非零数据点通过;若文档口径与实盘不符,以实盘为准并记录(参照 NAS 的 SYS.EFS 定案)
- [ ] AWS:S3 `BucketSizeBytes`/`NumberOfObjects` 经 ListMetrics 确认注册 + GetMetricData 实盘非零验证(可 90 天窗口,闲置实例记「未通过+原因」零值注记)
- [ ] aliyun:OSS bucket 级容量指标(`acs_oss`)实盘非零验证
- [ ] tencent/volcengine:探测并归因(指标订阅未开通 vs 指标不存在),记录二期补路径(参照 NAS volcengine 先例)
- [ ] 分布统计:实盘 OSS bucket 数/容量按厂商占比(双口径),判定是否触发升格/降级(占比 >15% 且探测可用→升格必达;否则维持尽力而为并记录理由)
- [ ] 产出探测报告 `docs/features/oss-ops-insight/probe-report.md`(namespace 定案/分组决策/降级预案),作为 T3/T4 依据

## Hard Rules

- 全程只读:探测只用真实账号只读 API,不写任何生产数据
- 必达/尽力而为分组必须由探测实盘证据支撑,不得凭实现便利臆断

## Implementation Notes

- 复用 `nasprobe` 的只读加载器与单位/分布工具,不重复造;OSS 专属 namespace 探测新增到对应厂商包(env 门控 manual probe 测试,参照 NAS `probe_manual_test.go` 模式)。
- 华为 `SYS.OBS` 文档口径需实盘验证——NAS 的 `SYS.SFS_Turbo` 实盘 0 上报先例证明文档不可全信。
- AWS S3 的 `BucketSizeBytes` 是公认标准指标;若实测 0 数据点(闲置 bucket),按「零值例外放行+打标」处理,不套用 CloudFront「主动放弃」先例。
- volcengine 若有指标订阅未开通问题,固化重试路径到探测脚本注释(参照 NAS probe §1.4)。
- 报告为发布 gate,未出报告不得进入 T3/T4。

---
id: "3"
title: "必达厂商 RDS 监控适配器(aliyun/huawei/aws)"
priority: "P0"
estimated_time: "2.5d"
complexity: "high"
dependencies: [1, 2]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 3: 必达厂商 RDS 监控适配器(aliyun/huawei/aws)

## Description

实现必达三家(由 T1 探测定案,默认 aliyun/huawei/aws)的 RDSMetricQuerier:aliyun `acs_rds_dashboard`、华为 `SYS.RDS`、AWS `AWS/RDS`(CPUUtilization/FreeableMemory 换算/FreeStorageSpace/DatabaseConnections)。按实例真实 region 查询,单位归一化,内存换算,失败路径三分。

## Reference Files

- `docs/proposals/rds-ops-insight/proposal.md` — Proposed Solution 第 2 条(5 厂商实现/内存口径)、单位归一化、Key Risks
- `internal/shared/cloudx/aliyun/disk_metrics.go`: 阿里 CMS 适配器先例(namespace/维度/query)
- `internal/shared/cloudx/huawei/disk_metrics.go`: 华为 CES 适配器先例(namespace 定案/BatchListMetricData/coerce 值)
- `internal/shared/cloudx/aws/disk_metrics.go`: AWS CloudWatch 适配器先例(GetMetricData/派生 busy_share)
- `docs/features/rds-ops-insight/probe-report.md`: T1 探测定案(namespace/内存口径/多引擎,发布 gate 产物)

## Acceptance Criteria

- [ ] aliyun:按实例查询 CPU/内存/磁盘/连接数(探测确认的 `acs_rds_dashboard` 指标),单位归一;真实客户端构造(非 mock),透传断言实例真实 region/rds_id
- [ ] 华为:按探测定案 namespace(`SYS.RDS`)查询,四指标非零验证(探测已 PASS 的实例);走 SafeValueOf 显式报错,不经 rds adapter 的静默回退
- [ ] AWS `AWS/RDS`:GetMetricData `CPUUtilization`/`FreeableMemory`/`FreeStorageSpace`/`DatabaseConnections`,内存使用率按 T1 换算公式(从 FreeableMemory);失败返回 error(不套用 CloudFront 主动放弃先例)
- [ ] 三适配器均按实例真实 region 查询(不做全局推断),失败返回空不阻塞全流程;多引擎统一写 RDSMetric(engine 透传)
- [ ] 单位归一化:三使用率统一 0~100(内存换算公式固化);connections 保留绝对值
- [ ] 单测:各适配器指标查询/单位换算/内存派生/失败路径三分(调用失败→ERROR / 探测不支持→INFO / 真实无数据→空+nil);`go build ./...` 通过

## Hard Rules

- 华为指标路径必须按 T1 探测定案 namespace,不得回退到 rds adapter 的静态/静默逻辑
- 必达厂商必须实盘验证非零(T1 报告证据),不得仅凭文档实现
- 内存使用率换算(FreeableMemory)必须按 T1 确认公式,换算不可靠则打标缺失而非伪造

## Implementation Notes

- 参照 NAS/Disk 三适配器完成先例,RDS 是第五次平移;改对应厂商的 rds adapter 增加指标查询状态与测试注入钩子。
- 维度按厂商官方文档顺序(参照 Disk:aliyun 实例维度、华为 RDS 实例维度、AWS DBInstanceIdentifier);以 T1 探测定案为准。
- 复用 NAS/Disk 的 `coerceCESValue`(json.Number 回归,47da689)处理华为 SDK 保真解码。
- 新代码主逻辑覆盖 80~100%(真实客户端构造壳由 T1 保留的 probe_manual_test 覆盖)。

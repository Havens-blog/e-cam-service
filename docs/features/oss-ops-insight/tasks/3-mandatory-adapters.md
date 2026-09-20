---
id: "3"
title: "必达厂商 OSS 监控适配器(aliyun/huawei/aws)"
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

# 3: 必达厂商 OSS 监控适配器(aliyun/huawei/aws)

## Description

实现必达三家(由 T1 探测定案,默认 aliyun/huawei/aws)的 OSSMetricQuerier:aliyun CMS `acs_oss`、华为 CES `SYS.OBS`、AWS CloudWatch `AWS/S3 BucketSizeBytes/NumberOfObjects`。按实例/bucket 真实查询,单位归一化,失败路径三分。

## Reference Files

- `docs/proposals/oss-ops-insight/proposal.md` — Proposed Solution 第 2 条(5 厂商实现)、单位归一化、Key Risks
- `internal/shared/cloudx/aliyun/nas_metrics.go`: 阿里 CMS 适配器先例(namespace/维度/query)
- `internal/shared/cloudx/huawei/nas_metrics.go`: 华为 CES 适配器先例(SYS.EFS 定案/BatchListMetricData/coerce 值)
- `internal/shared/cloudx/aws/nas_metrics.go`: AWS CloudWatch 适配器先例(GetMetricData/StorageBytes)
- `internal/shared/cloudx/types/oss.go`: OSSBucket 字段语义
- `docs/features/oss-ops-insight/probe-report.md`: T1 探测定案(namespace/维度,发布 gate 产物)

## Acceptance Criteria

- [ ] aliyun `acs_oss`:按 bucket 查询容量/对象数指标,单位字节→GB;真实客户端构造(非 mock),透传断言实例真实 region/bucket
- [ ] 华为 `SYS.OBS`:按探测定案 namespace 查询,容量/对象数非零验证(探测已 PASS 的 bucket);走 SafeValueOf 显式报错,不经 OBS adapter 的静默回退
- [ ] AWS `AWS/S3`:GetMetricData `BucketSizeBytes`/`NumberOfObjects` 双指标,失败返回 error(不套用 CloudFront 主动放弃先例)
- [ ] 三适配器均按 bucket 真实查询(不做全局推断),失败返回空不阻塞全流程
- [ ] 单位归一化走共享 `types.BytesToGB`,不复制粘贴;数量级 [1MB,1PB] 门禁兼容
- [ ] 单测:各适配器指标查询/单位换算/失败路径三分(调用失败→ERROR / 探测不支持→INFO / 真实无数据→空+nil);`go build ./...` 通过

## Hard Rules

- 华为指标路径必须按 T1 探测定案 namespace,不得回退到 OBS adapter 的静态/静默逻辑
- 必达厂商必须实盘验证非零(T1 报告证据),不得仅凭文档实现

## Implementation Notes

- 参照 NAS 三适配器完成先例(nas_metrics.go × 3),OSS 是第三次平移;改对应厂商的 OSS/S3/OBS adapter 增加指标查询状态与测试注入钩子。
- 维度按厂商官方文档顺序(参照 NAS:aliyun `{"userId","fileSystemId"}`、华为 `efs_instance_id`);OSS 的维度以 T1 探测定案为准。
- 复用 NAS 的 `coerceCESValue`(json.Number 回归,47da689)处理华为 SDK 保真解码。
- 新代码主逻辑覆盖 80~100%(真实客户端构造壳由 T1 保留的 probe_manual_test 覆盖)。

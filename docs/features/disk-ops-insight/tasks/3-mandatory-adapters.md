---
id: "3"
title: "必达厂商 Disk 监控适配器(aliyun/huawei/aws)"
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

# 3: 必达厂商 Disk 监控适配器(aliyun/huawei/aws)

## Description

实现必达三家(由 T1 探测定案,默认 aliyun/huawei/aws)的 DiskMetricQuerier:aliyun `acs_ecs_dashboard` 磁盘指标、华为云硬盘 namespace(探测确认)、AWS `AWS/EBS`(VolumeReadBytes/VolumeWriteBytes/VolumeIdleTime + 使用率派生)。按实例真实 region 查询,单位归一化,失败路径三分。

## Reference Files

- `docs/proposals/disk-ops-insight/proposal.md` — Proposed Solution 第 2 条(5 厂商实现/使用率口径)、单位归一化、Key Risks
- `internal/shared/cloudx/aliyun/nas_metrics.go`: 阿里 CMS 适配器先例(namespace/维度/query)
- `internal/shared/cloudx/huawei/nas_metrics.go`: 华为 CES 适配器先例(SYS.EFS 定案/BatchListMetricData/coerce 值)
- `internal/shared/cloudx/aws/nas_metrics.go`: AWS CloudWatch 适配器先例(GetMetricData/StorageBytes)
- `docs/features/disk-ops-insight/probe-report.md`: T1 探测定案(namespace/使用率口径,发布 gate 产物)

## Acceptance Criteria

- [ ] aliyun:按磁盘查询使用率/IOPS/吞吐(探测确认的 `acs_ecs_dashboard` 磁盘指标),单位归一;真实客户端构造(非 mock),透传断言实例真实 region/disk_id
- [ ] 华为:按探测定案 namespace 查询,使用率/IOPS/吞吐非零验证(探测已 PASS 的磁盘);走 SafeValueOf 显式报错,不经 disk adapter 的静默回退
- [ ] AWS `AWS/EBS`:GetMetricData `VolumeReadBytes`/`VolumeWriteBytes`/`VolumeIdleTime`,使用率按 T1 派生公式或打标缺失;失败返回 error(不套用 CloudFront 主动放弃先例)
- [ ] 三适配器均按实例真实 region 查询(不做全局推断),失败返回空不阻塞全流程
- [ ] 单位归一化:usage_percent 统一 0~100;iops/throughput 保留原始单位(MB/s 归一);容量字节 → GB 走共享 `types.BytesToGB`,不复制粘贴
- [ ] 单测:各适配器指标查询/单位换算/使用率派生(如适用)/失败路径三分(调用失败→ERROR / 探测不支持→INFO / 真实无数据→空+nil);`go build ./...` 通过

## Hard Rules

- 华为指标路径必须按 T1 探测定案 namespace,不得回退到 disk adapter 的静态/静默逻辑
- 必达厂商必须实盘验证非零(T1 报告证据),不得仅凭文档实现
- AWS 使用率派生(VolumeIdleTime)必须按 T1 确认公式,派生不可靠则打标缺失而非伪造

## Implementation Notes

- 参照 NAS 三适配器完成先例(nas_metrics.go × 3),Disk 是第四次平移;改对应厂商的 disk adapter 增加指标查询状态与测试注入钩子。
- 维度按厂商官方文档顺序(参照 NAS:aliyun 磁盘维度、华为 disk 维度、AWS VolumeId);以 T1 探测定案为准。
- 复用 NAS 的 `coerceCESValue`(json.Number 回归,47da689)处理华为 SDK 保真解码。
- 新代码主逻辑覆盖 80~100%(真实客户端构造壳由 T1 保留的 probe_manual_test 覆盖)。

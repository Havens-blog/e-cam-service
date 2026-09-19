---
id: "3"
title: "必达厂商 NAS 监控适配器(aliyun/huawei/aws)"
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

# 3: 必达厂商 NAS 监控适配器(aliyun/huawei/aws)

## Description

实现 NASMetricQuerier 的必达厂商(aliyun CMS / huawei CES / aws CloudWatch)适配器,按实例真实 region 查询,在采集边界完成字节→GB 换算。必达分组以 T1 探测报告为准(华为以探测通过为前提,失败降级见 T4)。

## Reference Files
- `docs/proposals/nas-ops-insight/proposal.md` — Proposed Solution、单位归一化与字段语义、Constraints & Dependencies、Key Risks、Success Criteria
- `docs/features/nas-ops-insight/probe-report.md`(T1 产物):必达厂商指标名/namespace 定案
- `internal/shared/cloudx/aliyun/cdn_metrics.go`: CMS 调用模式参照
- `internal/shared/cloudx/huawei/cdn_metrics.go`: 华为 json.Number 解析教训(47da689)——响应 map 解析须处理 json.Number
- `internal/shared/cloudx/aws/cdn_metrics.go`: aws 现有空实现参照(需加 cloudwatch 依赖)
- `internal/shared/cloudx/types/nas.go`: NASInstance 模型

## Acceptance Criteria
- [ ] aliyun CMS 适配器:按实例 region 调 DescribeMetricList 取 NAS 容量/用量,字节→GB 换算,单测含换算正确性
- [ ] huawei CES 适配器:按实例真实 region、按文件系统类型(普通 SFS `SYS.SFS` / SFS_Turbo `SYS.SFS_Turbo`)查 BatchListMetricData;响应 map 元素解析正确处理 json.Number
- [ ] aws CloudWatch 适配器:go.mod 新增 `service/cloudwatch`,按实例 region 查 EFS `StorageBytes`;无 CloudFront「主动放弃」回退
- [ ] 三适配器均按实例 region 查询(不做全局 region 推断);华为指标路径不经过「单 region 静默回退 cn-north-4」逻辑
- [ ] 失败路径:调用失败(API 错误/超时/鉴权)返回空 + ERROR 并带 error 字段;单测覆盖
- [ ] `go build ./...` 与三厂商适配器包测试通过

## Hard Rules
- 必达分组以 T1 探测报告为准(不可绕过 M1 定案)
- 单位换算必须在适配器采集边界,禁止字节写入 GB 字段
- 华为指标路径用实例真实 region,不经过单 region 回退逻辑

## Implementation Notes
- 参照 CDN 指标适配器的「可选接口 + 弱厂商尽力而为」模式(`cloudx.CDNMetricQuerier` 类型断言)。
- 华为响应解析务必处理 `json.Number`(SDK 用 UseNumber 解码 map)——CDN 华为指标踩过的坑(commit 47da689),NAS 华为 CES 同为 map 解析。
- aws 需新增 `aws-sdk-go-v2/service/cloudwatch` 一行 go.mod,版本匹配主 SDK。
- 适配器单测用 mock/桩(不依赖真实云账号);保留 T1 的 `probe_manual_test.go` 作真实账号验证(SKIP gate)。
- 单位归一化工具函数(字节→GB)若三适配器共用,提取为 `types` 或 cloudx 共享辅助(供 T4 也复用)。

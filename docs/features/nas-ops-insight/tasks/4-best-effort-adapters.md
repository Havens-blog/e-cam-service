---
id: "4"
title: "尽力而为厂商 NAS 监控适配器(tencent/volcengine)+ 单位归一化工具"
priority: "P1"
estimated_time: "2d"
complexity: "medium"
dependencies: [1, 2, 3]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 4: 尽力而为厂商 NAS 监控适配器(tencent/volcengine)+ 单位归一化工具

## Description

实现 NASMetricQuerier 的尽力而为厂商(tencent monitor / volcengine cloudmonitor)适配器,并抽取单位归一化工具函数(字节→GB)供全部适配器复用。尽力而为语义:探测不可用则返回空 + INFO「探测不支持」,不阻塞全流程。

## Reference Files
- `docs/proposals/nas-ops-insight/proposal.md` — Proposed Solution、单位归一化与字段语义、失败可观测性、Key Risks、Success Criteria
- `docs/features/nas-ops-insight/probe-report.md`(T1 产物):tencent/volcengine 指标名定案或「二期补」判定
- `internal/shared/cloudx/tencent/cdn_metrics.go`: 腾讯 CDN 指标参照
- `internal/shared/cloudx/volcano/cdn_metrics.go`: 火山 CDN 指标参照
- `internal/shared/cloudx/aliyun/cdn_metrics.go` + `internal/shared/cloudx/huawei/cdn_metrics.go`: 单位换算/数量级自检复用(T3 已实现)

## Acceptance Criteria
- [ ] tencent monitor 适配器:go.mod 新增 `tencentcloud-sdk-go/tencentcloud/monitor` 子包;按 T1 定案指标实现,字节→GB 换算
- [ ] volcengine cloudmonitor 适配器:按 T1 定案指标名实现;探测不可用则返回空 + INFO「探测不支持」(不报 ERROR)
- [ ] 单位归一化工具函数提取:字节→GB(/1024^3)共享函数,数量级自检 [1MB,1PB] + capacity=0 例外放行打 `qc_status=zero_exception`,T3/T4 适配器共用
- [ ] 失败路径:调用失败返回空 + ERROR 并带 error 字段;探测不支持返回空 + INFO;单测覆盖两条路径
- [ ] 若 T1 判定 tencent/volcengine 某厂商「二期补」:该厂商返回空 + INFO,并在代码注释与任务 Result 记录判定来源
- [ ] `go build ./...` 与适配器包测试通过

## Hard Rules
- 尽力而为:探测不可用返回空 + INFO,不得报 ERROR 也不得假装有数据
- 单位换算/数量级自检复用 T3 提取的共享工具,不得复制粘贴
- 华为探测失败时的降级(华为转尽力而为)不在此任务范围——属 T1 探测报告定案 + T3 条件式实现

## Implementation Notes
- tencent 需新增 `monitor` 子包依赖(一行 go.mod,版本匹配主 SDK v1.3.38)。
- volcengine 已在主 SDK(`volcengine-go-sdk` 含 cloudmonitor),零新依赖。
- 单位归一化工具若 T3 已提取则直接复用;若 T3 未提取,本任务在共享位置(如 `internal/shared/cloudx/nasmetric` 或 types 包)提取并让 T3/T4 共用。
- 风险提示(proposal Key Risks):volcengine 监控文档稀缺;升格判定(占比>15%)由 T1 探测报告给出,不在本任务重判。

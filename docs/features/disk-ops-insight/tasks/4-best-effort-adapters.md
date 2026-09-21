---
id: "4"
title: "尽力而为厂商 Disk 监控适配器(tencent/volcengine)"
priority: "P1"
estimated_time: "1.5d"
complexity: "medium"
dependencies: [1, 2]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 4: 尽力而为厂商 Disk 监控适配器(tencent/volcengine)

## Description

实现尽力而为两家(tencent/volcengine)的 DiskMetricQuerier:腾讯 monitor(CBS 或 `QCE/CVM` 磁盘指标)、火山 cloudmonitor。若 T1 探测显示某厂商指标订阅未开通,实现为「二期补」桩(空+INFO,不伪装数据)。

## Reference Files

- `docs/proposals/disk-ops-insight/proposal.md` — Proposed Solution 第 2 条(尽力而为分组)、Key Risks
- `internal/shared/cloudx/tencent/nas_metrics.go`: 腾讯 monitor 适配器先例(QCE 查询/appid 解析缓存)
- `internal/shared/cloudx/volcano/nas_metrics.go`: 火山「二期补」桩先例(空+INFO+重试路径注释)
- `internal/shared/cloudx/tencent/oss_metrics.go`: 腾讯 OSS 适配器先例(单位换算/MB 陷阱)
- `docs/features/disk-ops-insight/probe-report.md`: T1 探测结论(可用/不可用)

## Acceptance Criteria

- [ ] tencent:云盘使用率/IOPS/吞吐查询(探测确认的 namespace,如 CBS 或 CVM 磁盘指标),单位归一;真实客户端构造,失败返回 error
- [ ] volcengine:按 T1 结论——可用则真实实现,不可用则「二期补」桩(空切片+nil error+INFO,不打 ERROR 不假装有数据)
- [ ] 任一适配器失败只返回自身空,不阻塞全流程
- [ ] 单位归一化复用共享函数,不复制粘贴(Hard Rule);注意单位陷阱(如 MB vs byte,参照 OSS StdStorage 先例)
- [ ] 失败双路径单测:调用失败→ERROR+error 字段+返回 error;探测不支持→INFO+空+nil;真实无数据点→空+nil
- [ ] `go build ./...` 通过

## Hard Rules

- 尽力而为厂商探测不可用必须显式降级「二期补」并固化重试路径注释,不得伪装数据或打 ERROR 噪音
- 单位归一化必须复用共享函数,不得复制粘贴;注意厂商单位陷阱(参照 OSS StdStorage MB 先例)

## Implementation Notes

- 参照 NAS/OSS 的 tencent/volcano 适配器完成先例,Disk 第四次平移。
- tencent 的 appid 经 CAM `GetUserAppId` 解析缓存(参照 NAS);维度以 T1 探测为准。
- 若 T1 显示 tencent/volcengine 任一占比 >15% 且探测可用,此任务升格为必达(在 T1 报告中记录升格决策)。

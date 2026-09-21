---
status: "completed"
started: "2026-09-21 11:55"
completed: "2026-09-21 12:06"
time_spent: "~11m"
---

# Task Record: 4 尽力而为厂商 Disk 监控适配器(tencent/volcengine)

## Summary
尽力而为两家(tencent/volcengine)DiskMetricQuerier 按 T1 探测定案实现为「二期补」桩:probe-report §1.4(QCE/CBS 云盘 IO/使用率指标未注册 + 云监控数据查询权限未开通,8 候选×5 盘全 invalid,占比 0.49%/0.36% 不触发升格)与 §1.5(2240 组合候选矩阵 metric not found,订阅未开通,占比 50.96%/46.33% >15% 但探测不可用 → 显式降级二期补)。两桩均为空切片+nil error+INFO 日志(不打 ERROR、不伪装数据),重试路径(开通订阅/权限 → 重跑 TestManualProbeTencentQCECBSMetrics / TestManualProbeVolcanoEBSCloudMonitor → 按文档候选定案)已固化在文件头注释,并注明二期补落地时吞吐换算须走共享 types.BytesPerSecToMBPerSec、监控客户端按实例真实 region 创建。新增 4 单测(探测不支持→空+nil,含无参调用),go build ./... 通过,两包全量测试 ok。

## Changes

### Files Created
- internal/shared/cloudx/tencent/disk_metrics.go
- internal/shared/cloudx/tencent/disk_metrics_test.go
- internal/shared/cloudx/volcano/disk_metrics.go
- internal/shared/cloudx/volcano/disk_metrics_test.go

### Files Modified
无

### Key Decisions
- SPEC CONTRADICTION 处置:任务 AC 第 1 条预设 tencent 真实客户端构造,但 T1 probe-report §1.4/§4(Reference File,更权威)定案探测未通过且 T4 不纳入本期;按 Hard Rules(探测不可用须显式降级二期补、不伪装数据)+ 任务 Description 条件分支,tencent 与 volcengine 均实现为二期补桩,与 volcano NAS/OSS 桩先例同型
- 升格判定:volcengine 占比 >15% 但探测不可用、tencent 占比 ≤15% 且探测不可用,「>15% 且探测可用」双条件无一满足,不升格(§3 升格判定表)
- 失败三分路径中的「调用失败→ERROR+error」「真实无数据点→空+nil」在桩中不存在(无真实查询路径),已在两处文件头重试路径注释固化「二期补落地时补齐失败三分路径单测」,先例与 volcano nas_metrics_test.go 同型

## Test Results
- **Tests Executed**: Yes
- **Passed**: 4
- **Failed**: 0
- **Coverage**: 100.0%

## Acceptance Criteria
- [x] tencent:云盘使用率/IOPS/吞吐查询(T1 探测未通过 → 按任务 Description/Hard Rules 降级二期补桩:空+INFO+nil,重试路径固化;真实客户端构造分支前置条件未成立)
- [x] volcengine:按 T1 结论——探测不可用 → 二期补桩(空切片+nil error+INFO,不打 ERROR 不假装有数据)
- [x] 任一适配器失败只返回自身空,不阻塞全流程(两桩均空切片+nil,单测断言)
- [x] 单位归一化复用共享函数:桩无换算路径不存在复制粘贴;二期补重试注释固化吞吐须走 types.BytesPerSecToMBPerSec(与 T3 三厂商共用)
- [x] 失败双路径单测:探测不支持→INFO+空+nil 双包 4 用例全绿;另两路径随二期补真实实现落地并已在注释固化补测要求
- [x] go build ./... 通过

## Notes
just lint 报 3 errors+16 warnings 全部位于 e-cam-web 前端 TS(pre-existing,与本次 Go 改动无关);just fmt 仅触碰 pre-existing 文件(纯换行符噪音),新增 4 文件 gofmt -l 干净。新增函数覆盖 100%(go tool cover -func:两包 GetDiskMetrics 各 100.0%);整包语句覆盖率 0.1%/0.2% 因两包为大体量既有适配器包,不代表本次改动覆盖。

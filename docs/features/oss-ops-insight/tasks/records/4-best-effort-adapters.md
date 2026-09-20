---
status: "completed"
started: "2026-09-20 12:24"
completed: "2026-09-20 12:34"
time_spent: "~10m"
---

# Task Record: 4 尽力而为厂商 OSS 监控适配器(tencent/volcengine)

## Summary
尽力而为厂商 OSS 监控适配器(tencent/volcengine)完成:tencent COSAdapter.GetOSSMetrics 按 M1 探测定案实现(QCE/COS namespace、StdStorage(MB)/StdObjectNumber、维度 bucket 单维、Period 86400,窗口/聚合辅助复用 NAS 同包实现);volcengine TOSAdapter.GetOSSMetrics 按探测结论(订阅未开通)实现「二期补」桩(空切片+nil error+INFO,重试路径固化);新增共享 types.MBToGB(委托 BytesToGB,处理 tencent MB 口径,遗留行动 #4);12 个新单测全绿(失败双路径/换算锚点/请求形状/region 透传与兜底/客户端构造失败)。

## Changes

### Files Created
- internal/shared/cloudx/tencent/oss_metrics.go
- internal/shared/cloudx/tencent/oss_metrics_test.go
- internal/shared/cloudx/volcano/oss_metrics.go
- internal/shared/cloudx/volcano/oss_metrics_test.go

### Files Modified
- internal/shared/cloudx/tencent/cos.go
- internal/shared/cloudx/types/oss_metric.go
- internal/shared/cloudx/types/oss_metric_test.go

### Key Decisions
- tencent 实现挂在 COSAdapter(对齐 aliyun OSSAdapter/huawei OBSAdapter/aws S3Adapter 命名先例);volcengine 挂 TOSAdapter
- MB→GB 新增共享 types.MBToGB,内部委托 types.BytesToGB(MB→byte→GB 单一换算链,Hard Rule 不复制粘贴);禁止把 MB 当 byte 进 BytesToGB(probe-report 遗留行动 #4)
- QCE/COS 维度按 probe 定案用 bucket 单维,不发起 CAM GetUserAppId 调用(NAS 的 appid 双维路径不需要)
- OSSMetricQuerier 无 region 签名:monitor 客户端按账号 defaultRegion 创建(空则兜底 ap-guangzhou,对齐 aliyun CMS 先例);region 不一致导致无数据按「真实无数据点」空+nil 可观测呈现
- 窗口/时间格式/聚合辅助复用同包 NAS 指标实现(nasMetricDateRange/nasMonitorRangeBounds/formatNasMonitorTime/aggregateNASMonitorDaily),92 天窗口上限同 NAS
- volcengine 桩按 probe-report §1.5 固化三步重试路径:开通订阅→重跑 TestManualProbeVolcanoTOSCloudMonitor→按文档候选替换真实查询

## Test Results
- **Tests Executed**: Yes
- **Passed**: 12
- **Failed**: 0
- **Coverage**: 91.3%

## Acceptance Criteria
- [x] tencent QCE/COS bucket 级容量/对象数查询,单位换算走共享函数;真实客户端构造,失败返回 error
- [x] volcengine 按探测不可用实现「二期补」桩(空切片+nil error+INFO,不打 ERROR 不假装有数据)
- [x] 任一适配器失败只返回自身空,不阻塞全流程
- [x] 单位归一化复用共享函数(types.MBToGB 委托 types.BytesToGB),不复制粘贴
- [x] 失败双路径单测:调用失败→ERROR+error;探测不支持→INFO+空+nil;真实无数据点→空+nil
- [x] go build ./... 通过

## Notes
coverage 91.3 为 tencent GetOSSMetrics 新代码块覆盖(buildTencentOSSMetrics/fetchCOSMonitorDaily/logOSSMetricFailure/monitorRegion/MBToGB 均 100%,volcano 桩 100%,types 包 96.2%);全包数值混入既有适配器不具可比性。静态检查:go build ./... 与 go vet(变更包)通过;Makefile 无 compile 目标(equivalent gate=go build);make lint 本身损坏(未设 GOLINT 变量致 bash 语法错),golangci-lint 未安装。make fmt 触及既有文件的整仓 CRLF/格式化 churn(245 个既有 M),非本次修改文件。实盘 QCE/COS 活体查询由 env 门控 oss_probe_manual_test 覆盖(与 NAS/CDN 同口径)。

---
status: "completed"
started: "2026-09-19 16:21"
completed: "2026-09-19 16:40"
time_spent: "~19m"
---

# Task Record: 4 尽力而为厂商 NAS 监控适配器(tencent/volcengine)+ 单位归一化工具

## Summary
实现尽力而为厂商 NAS 监控适配器(tencent monitor 真实查询路径 + volcengine「二期补」桩)+ 复用 T3 单位归一化工具。tencent:go.mod 新增 tencentcloud/monitor v1.3.182(主 common SDK 同步升级 1.3.82→1.3.182,全仓编译通过);按官方监控指标文档定案 Namespace=QCE/CFS、MetricName=Storage/StorageUsage、维度 appid+FileSystemId(appid 经 CAM GetUserAppId 解析并缓存,仿 aliyun resolveUserID 先例);字节→GB 在采集边界经共享工具 types.BytesToGB 换算(Hard Rule:不复制粘贴);总容量无直接指标,按 used÷(StorageUsage/100) 派生,usage≤0 → capacity=0(写路径打 zero_exception,禁 NaN,与华为派生同构)。volcengine:按 T1 探测定案「二期补」实现为桩——返回空切片+nil error+INFO「探测不支持」,不打 ERROR、不假装有数据,代码注释与判定来源(probe-report §1.4/§3/§4)及二期补重试路径(开通订阅→重跑 TestManualProbeVolcanoNASCloudMonitor→按文档候选定案)已固化。失败双路径均覆盖单测:调用失败→ERROR+error 字段+返回 error(执行器记失败计数);探测不支持→INFO+空+nil;真实无数据点→空切片+nil(不触发失败计数)。

## Changes

### Files Created
- internal/shared/cloudx/tencent/nas_metrics.go
- internal/shared/cloudx/tencent/nas_metrics_test.go
- internal/shared/cloudx/volcano/nas_metrics.go
- internal/shared/cloudx/volcano/nas_metrics_test.go

### Files Modified
- internal/shared/cloudx/tencent/cfs.go
- go.mod
- go.sum

### Key Decisions
- tencent 指标名 T1 未定案(0 实例无法实测),按官方监控指标文档定案 QCE/CFS + Storage/StorageUsage + appid/FileSystemId;appid 为 CFS 云监控维度必带项,经 CAM GetUserAppId 解析(进程内 sync.Once 缓存,测试钩子注入)
- 官方文档标注 Storage 单位为 GB 而 proposal「换算对照」定案为字节,按 Reference Files 优先级取字节→GB 换算;0 实例无法活体核验,若实测单位偏差,DAO [1MB,1PB] 数量级自检会把错行整批拒绝进入失败计数(可观测不静默),届时按实测修正
- volcengine 按 probe-report §1.4 定案「二期补」:桩实现返回空+INFO,不建真实查询路径(重试路径已固化在注释中,与探测脚本呼应)
- 单位归一化工具(AC-3)T3 已提取(types.BytesToGB + DAO nasMetricQC [1MB,1PB] 门禁 + zero_exception 打标),本任务直接复用,未重复提取

## Test Results
- **Tests Executed**: Yes
- **Passed**: 12
- **Failed**: 0
- **Coverage**: 78.6%

## Acceptance Criteria
- [x] tencent monitor 适配器:go.mod 新增 monitor 子包;按 T1 定案指标实现,字节→GB 换算
- [x] volcengine cloudmonitor 适配器:按 T1 定案(二期补)返回空 + INFO「探测不支持」,不报 ERROR
- [x] 单位归一化工具:复用 T3 已提取的 types.BytesToGB + DAO 数量级自检 [1MB,1PB] + zero_exception,T3/T4 共用,不复制粘贴
- [x] 失败路径:调用失败返回空 + ERROR 并带 error 字段;探测不支持返回空 + INFO;单测覆盖两条路径
- [x] T1 判定 volcengine「二期补」:返回空 + INFO,代码注释与 Result 记录判定来源(probe-report §1.4/§3,2026-09-19 实测)
- [x] go build ./...(逐包)与适配器包测试通过(tencent/volcano 全绿)

## Notes
测试覆盖:tencent nas_metrics.go 函数级覆盖均值约 78.6%(12.5% 的 createNASMonitorClient 为真实 SDK 客户端构造路径,按既有三厂商适配器先例不入桩测试;纯解析/聚合/组装函数 100%);volcano 包 -cover 因既有 volcano/kafka.go BOM 无法插桩(已登记既有隐患),普通测试绿。既有环境注记:gofmt -s -l 对两包全部既有文件报 CRLF(仓库既有状态,新文件均为 LF 格式干净);golangci-lint 未安装,以 go vet(通过)替代;全仓 build 曾在 cmd/tmp-volcano-import 处 OOM(孤儿 link.exe 占内存),杀孤儿后逐包 build 全通过。判定来源记录(AC-5):volcengine 二期补 —— probe-report §1.4(cloudmonitor 指标注册表为空/订阅未开通,355+ 候选组合 metric not found)+ §3(实例数占比 12.6%、容量原值占比 14.40%,双口径 ≤15% 且探测不可用 → 不触发升格)。

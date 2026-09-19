---
status: "completed"
started: "2026-09-19 14:11"
completed: "2026-09-19 16:18"
time_spent: "~2h 7m"
---

# Task Record: 3 必达厂商 NAS 监控适配器(aliyun/huawei/aws)

## Summary
实现必达三厂商 NASMetricQuerier 适配器:aliyun CMS DescribeMetricList(acs_nas,极速型 ExtremeCapacity/ExtremeCapacityUsed + 通用型 AlignedSize 按数据择优,Dimensions 按 userId+fileSystemId 文档顺序,userId 经 STS GetCallerIdentity 解析缓存)、huawei CES BatchListMetricData(实例真实 region SafeValueOf 无 cn-north-4 回退;单批查询 SYS.EFS[M1 实测定案 primary]+SYS.SFS 兼容候选 × used_capacity/used_capacity_percent,维度 efs_instance_id;总容量按 used÷percent 派生,percent≤0→0 禁 NaN;json.Number 容忍提取器以 SDK 真实解码路径回归 47da689)、aws CloudWatch GetMetricData(AWS/EFS StorageBytes,go.mod 提升 service/cloudwatch v1.45.2 为直接依赖,失败返回 error 显式区别 CloudFront 主动放弃,EFS 无总容量指标→capacity=0)。三适配器均按实例 region 查询(region 透传断言,不做全局推断);字节→GB 换算统一 types.BytesToGB 在采集边界完成(nasprobe.BytesToGB 委托同源);失败路径 ERROR+error 字段+返回 error,真实无数据返回空切片不触发失败计数。缺失日跳过不落库,同日多点取最后(日末态)。30 个新单测全绿,新代码主逻辑覆盖率 80~100%(真实客户端构造壳需真实凭证,由 T1 probe_manual_test 覆盖)

## Changes

### Files Created
- internal/shared/cloudx/aliyun/nas_metrics.go
- internal/shared/cloudx/aliyun/nas_metrics_test.go
- internal/shared/cloudx/huawei/nas_metrics.go
- internal/shared/cloudx/huawei/nas_metrics_test.go
- internal/shared/cloudx/aws/nas_metrics.go
- internal/shared/cloudx/aws/nas_metrics_test.go

### Files Modified
- internal/shared/cloudx/types/nas_metric.go
- internal/shared/cloudx/nasprobe/probe.go
- internal/shared/cloudx/aliyun/nas.go
- internal/shared/cloudx/huawei/sfs.go
- internal/shared/cloudx/aws/efs.go
- go.mod

### Key Decisions
- 华为 namespace 取 SYS.EFS 为主候选(SYS.SFS/SYS.SFS_Turbo 文档口径在实盘 0 上报,M1 探测定案优先于 AC 原文的文档口径表述;SYS.SFS 保留作普通 SFS 兼容候选,单批同查按数据择优,避免串行回退)
- aliyun 不预知 fs 类型,以指标数据择优:ExtremeCapacityUsed 无数据点回退 AlignedSize;通用型无真实总容量指标→capacity=0 走 zero_exception(实盘 capacity=10485760 为名义上限不可信)
- CMS NAS 指标维度需 userId(官方文档顺序),经 STS GetCallerIdentity 进程内缓存解析
- 单位换算提取 types.BytesToGB 共享辅助(供 T4 复用),nasprobe 委托至同一实现
- 华为 json.Number 防呆:coerceCESValue 容忍 float64/json.Number/字符串,测试用华为 SDK 真实解码路径(utils.Unmarshal=jsoniter UseNumber)验证 47da689 同类缺陷

## Test Results
- **Tests Executed**: Yes
- **Passed**: 30
- **Failed**: 0
- **Coverage**: 80.0%

## Acceptance Criteria
- [x] aliyun CMS 适配器:按实例 region 调 DescribeMetricList 取容量/用量,字节→GB 换算,单测含换算正确性
- [x] huawei CES 适配器:按实例真实 region、按文件系统类型查 BatchListMetricData;响应 map 元素解析正确处理 json.Number
- [x] aws CloudWatch 适配器:go.mod 新增 service/cloudwatch,按实例 region 查 EFS StorageBytes;无 CloudFront 主动放弃回退
- [x] 三适配器均按实例 region 查询;华为指标路径不经过单 region 静默回退 cn-north-4
- [x] 失败路径:调用失败返回空+ERROR 带 error 字段;单测覆盖
- [x] go build ./... 与三厂商适配器包测试通过

## Notes
lint 登记环境限制:golangci-lint 未安装、staticcheck(1.24.1)与模块 go1.25.5 不兼容,go vet 通过;gofmt 对仓库预存量文件因 CRLF 全量标记(既有状态),本任务触碰文件已全部格式化。华为/总容量派生、AWS 零值实例等口径以 probe-report M1 定案为准;AccountID 由 T4 执行器回填

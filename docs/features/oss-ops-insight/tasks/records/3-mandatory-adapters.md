---
status: "completed"
started: "2026-09-20 11:59"
completed: "2026-09-20 12:22"
time_spent: "~23m"
---

# Task Record: 3 必达厂商 OSS 监控适配器(aliyun/huawei/aws)

## Summary
实现必达三家 OSSMetricQuerier 适配器:aliyun CMS acs_oss_dashboard(MeteringStorageUtilization/ObjectCount,维度 BucketName,Period=3600,31 天窗口守卫)、huawei CES SYS.OBS(capacity_total/object_num_all,维度 bucket_name,BatchListMetricData,SafeValueOf 显式报错不走 OBS 静默回退)、aws CloudWatch AWS/S3(GetMetricData 双指标 BucketSizeBytes/StandardStorage + NumberOfObjects/AllStorageTypes,GetBucketLocation 解析 bucket 真实 region,不套用 CloudFront 主动放弃先例)。三适配器均按 bucket 逐桶真实查询、失败路径三分(调用失败→ERROR+error / 真实无数据→空+nil / 缺值跳过不伪造 0),单位归一化统一走共享 types.BytesToGB。新增 OSSMetricQuerier 签名锚测试冻结接口形状。

## Changes

### Files Created
- internal/shared/cloudx/aliyun/oss_metrics.go
- internal/shared/cloudx/aliyun/oss_metrics_test.go
- internal/shared/cloudx/huawei/obs_metrics.go
- internal/shared/cloudx/huawei/obs_metrics_test.go
- internal/shared/cloudx/aws/oss_metrics.go
- internal/shared/cloudx/aws/oss_metrics_test.go
- internal/shared/cloudx/oss_metric_querier_test.go

### Files Modified
- internal/shared/cloudx/aliyun/oss.go
- internal/shared/cloudx/huawei/obs.go
- internal/shared/cloudx/huawei/nas_metrics.go
- internal/shared/cloudx/aws/s3.go

### Key Decisions
- SPEC CONTRADICTION 处置:proposal 写 aliyun namespace=acs_oss,probe-report(更新,M1 gate 产物)实测定案 acs_oss_dashboard 实盘成立而 acs_oss 基本失效——按较新 Reference File 取 acs_oss_dashboard
- OSSMetricQuerier 签名无 region(接口冻结):aliyun CMS 指标查询与 bucket region 无关按账号 defaultRegion;huawei CES 按账号 defaultRegion + SafeValueOf 显式报错(Hard Rule);aws CloudWatch 按 bucket 所在 region 上报,适配器内部经 GetBucketLocation 解析真实 region,解析失败显式报错不静默回退
- aliyun 计量类指标 31 天窗口限制在适配器显式守卫(超窗报错,防静默空查询,probe-report §1.1 遗留行动 #3)
- coerceCESValue 扩展 *float64 分支(向后兼容)统一 OBS 数据点值提取防呆链(47da689 同型,json.Number 回归保留)
- 真实客户端构造壳(需网络/真实凭证)不做单测,由 T1 保留的 oss_probe_manual_test(env 门控)覆盖,与 Implementation Notes 口径一致

## Test Results
- **Tests Executed**: Yes
- **Passed**: 25
- **Failed**: 0
- **Coverage**: 78.3%

## Acceptance Criteria
- [x] aliyun acs_oss_dashboard 按 bucket 查询容量/对象数,单位字节→GB,真实客户端构造,透传真实 bucket
- [x] huawei SYS.OBS 按探测定案查询,SafeValueOf 显式报错不走 OBS adapter 静默回退
- [x] AWS GetMetricData BucketSizeBytes/NumberOfObjects 双指标,失败返回 error(不套用 CloudFront 主动放弃)
- [x] 三适配器按 bucket 真实查询,失败返回空不阻塞全流程
- [x] 单位归一化走共享 types.BytesToGB,[1MB,1PB] 门禁兼容(DAO 写路径)
- [x] 单测覆盖指标查询/单位换算/失败路径三分,go build ./... 通过

## Notes
新代码主逻辑块覆盖 78.3%(aliyun 80.9%/huawei 82.0%/aws 71.7%);aws 缺口为需真实网络的 GetBucketLocation/CloudWatch 构造壳(逻辑经钩子注入覆盖,live 由 oss_probe_manual_test 覆盖)。regression:internal/shared/cloudx/... + cam executor/dao 24 包全绿;just compile/vet 通过;just fmt/lint 仅标记既有无关文件(logquery mappers、e-cam-web TS)。华为 CES SafeBuild 会自动请求 IAM 解析 project id,离线单测不可行,已注释说明。

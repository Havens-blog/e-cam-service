---
status: "completed"
started: "2026-09-21 11:19"
completed: "2026-09-21 11:53"
time_spent: "~34m"
---

# Task Record: 3 必达厂商 Disk 监控适配器(aliyun/huawei/aws)

## Summary
必达三厂 Disk 监控适配器落地(第四次平移 NAS 指标模式):aliyun acs_ecs_dashboard(diskId 维度 DiskRead/WriteIOPS + DiskRead/WriteBPS,使用率走挂载实例级 vm.DiskUtilization,ECS 资产路径解析挂载实例,instance_level 打标)、huawei SYS.EVS disk_device_* 系列(ListMetrics 前缀发现 <盘ID>-<设备名> 维度键 + BatchListMetricData 5 指标批量,使用率 disk_device_io_util 打 instance_level,读序列未命中回退写序列)、aws AWS/EBS(GetMetricData 5 指标 Sum 日粒度,IOPS/吞吐按 86400s 窗口归一,VolumeIdleTime 经共享 types.DiskBusySharePercentFromIdle 派生 busy_share,派生越界打标缺失)。三家均按实例真实 region 创建监控客户端(huawei 走 cesv1region.SafeValueOf 显式报错不回退默认 region),失败路径三分(调用失败 ERROR+error / 真实无数据 空切片+nil / huawei 无设备序列 INFO+空),byte/s→MB/s 经共享 types.BytesPerSecToMBPerSec 单点换算,usage_percent 0~100 交 DAO 门禁。DiskMetricQuerier 断言 + 测试注入钩子齐备,26 个新单测全绿。

## Changes

### Files Created
- internal/shared/cloudx/aliyun/disk_metrics.go
- internal/shared/cloudx/aliyun/disk_metrics_test.go
- internal/shared/cloudx/huawei/disk_metrics.go
- internal/shared/cloudx/huawei/disk_metrics_test.go
- internal/shared/cloudx/aws/disk_metrics.go
- internal/shared/cloudx/aws/disk_metrics_test.go

### Files Modified
- internal/shared/cloudx/aliyun/disk.go
- internal/shared/cloudx/huawei/disk.go
- internal/shared/cloudx/aws/disk.go
- internal/shared/cloudx/aws/nas_metrics.go
- internal/shared/cloudx/types/disk_metric.go

### Key Decisions
- aliyun 使用率按 probe-report §1.1 实盘形态实现:ECS DescribeDisks 解析挂载实例后以 instanceId 单维查 vm.DiskUtilization(实盘探测 PASS 形态,非文档全维形态);未挂载盘不查使用率留 0 交写路径 zero_exception
- huawei 使用率选 SYS.EVS disk_device_io_util(可按盘 ID 前缀匹配关联)而非 SYS.ECS disk_util_inband(需实例 ID 维度,接口签名无实例上下文),按 probe-report §2 归一方案打 instance_level
- AWS 派生公式落 types.DiskBusySharePercentFromIdle 共享函数(nasprobe 包注释 Hard Rule 禁生产 import,故在 types 建生产唯一事实源,nasprobe 探测产物不动);idle 缺失/越界日使用率 0+不打标(派生不可靠打标缺失而非伪造)
- byte/s→MB/s 归一新增 types.BytesPerSecToMBPerSec 共享函数(1024 进位与 BytesToGB 同口径),三家适配器单点调用不复制粘贴
- aws newCloudWatchClient 抽包级共享函数,EFSAdapter.createCWClient 与 DiskAdapter.createEBSClient 共用(DRY);huawei CES 客户端构造含 IAM project-id 网络调用离线不可测,构造壳由 disk_probe_manual_test env 门控覆盖(与任务 Implementation Notes 一致)
- 未采集 acs_ecs_dashboard *BurstUtilization 系列,天然规避实盘 -1 哨兵值(probe-report 遗留行动 #4),已在适配器注释留二期扩采警示

## Test Results
- **Tests Executed**: Yes
- **Passed**: 285
- **Failed**: 0
- **Coverage**: 86.3%

## Acceptance Criteria
- [x] aliyun:按磁盘查询使用率/IOPS/吞吐(acs_ecs_dashboard),单位归一;真实客户端构造,透传断言真实 region/disk_id
- [x] 华为:按探测定案 namespace(SYS.EVS)查询,使用率/IOPS/吞吐实盘口径;SafeValueOf 显式报错不静默回退
- [x] AWS AWS/EBS:五指标 GetMetricData,使用率按 T1 派生公式打标 busy_share(不可靠则打标缺失);失败返回 error 不套用 CloudFront 放弃先例
- [x] 三适配器均按实例真实 region 查询,失败返回空不阻塞全流程
- [x] 单位归一化:usage_percent 0~100;iops 原始单位;throughput MB/s 经共享 types.BytesPerSecToMBPerSec 归一
- [x] 单测:指标查询/单位换算/使用率派生/失败路径三分(调用失败 ERROR/无序列 INFO/真实无数据空+nil);go build ./... 通过

## Notes
coverage 86.3% = 三家 disk_metrics.go 合并(go tool cover -func 合并 profile),主逻辑 build 函数 100%、GetDiskMetrics 74.5~91.7%;未达部分为真实客户端构造壳(IAM/project-id 网络调用离线不可测,按任务 Implementation Notes 由 probe_manual_test 覆盖)。types 包 91.7%。go test -race 该宿主无 cgo 不可用(既有环境约束);lint 以 go vet 承接,任务四包 vet 全绿,全仓 vet 仅 internal/logquery/cdncache 既有失败(并行 LQO feature 在途树,与本任务无关)。测试数 285 PASS / 8 SKIP(env 门控 manual probe + live DAO,按设计跳过)/ 0 FAIL,统计自 aliyun/huawei/aws/types 四包 go test -v。

---
status: "completed"
started: "2026-09-20 10:32"
completed: "2026-09-20 11:37"
time_spent: "~1h 5m"
---

# Task Record: 1 OSS 厂商监控 API 探测:namespace/指标名实盘验证 + 分布统计定分组

## Summary
OSS M1 探测定案完成:新增 nasprobe/oss_usage.go 共享纯逻辑(OSSBucketsToUsage 双口径聚合/OSSGrowthPercent/IsHighGrowth,12 子用例 100% 覆盖)+ 5 厂商 env 门控 oss_probe_manual_test.go + 5 厂商 bucket 分布统计探针,用真实账号完成全部只读探测并产出 docs/features/oss-ops-insight/probe-report.md。核心定案:①必达=aliyun/huawei/aws 三家实盘非零全通过——aliyun 文档口径 namespace acs_oss 实盘已基本失效(400-10002 归因),现行可用为 acs_oss_dashboard/MeteringStorageUtilization(byte)+ObjectCount,维度 BucketName,Period=3600,窗口≤31天(与 NAS「文档不可全信」先例同型);华为 SYS.OBS 文档口径实盘成立,维度 bucket_name,capacity_total(byte) 58 序列非零且与 GetBucketStat 数据面逐桶互证一致(fat-jlc-pub-file 6042.2418 GB 两路相等);aws AWS/S3 BucketSizeBytes(StandardStorage)+NumberOfObjects(AllStorageTypes) 84/84 注册序列非零(90 天 89 日点),20 个未注册 bucket 走零值注记;②tencent 修正假设:QCE/COS 探测可用(DescribeBaseMetrics 206 指标,StdStorage 单位是 MB 非 byte,维度 bucket 单维生效),无订阅障碍;③volcengine 归因「云产品监控指标订阅未开通」(289 候选组合 metric not found + ECS 阳性对照否定),二期补重试路径固化在探测脚本;④分布 995 bucket:aliyun 572(57.49%,容量 93.72%)/huawei 142(14.27%)/volcano 155(15.58%)/aws 104(10.45%)/tencent 22(2.21%),双口径呈现且容量口径不可信范围已标注(TOS 无统计接口恒 0,aws/tencent 快照全 0);⑤升格判定:双条件「>15% 且探测可用」无一满足,tencent 维持尽力而为(占比不足但探测可用,T4 可直接实现),volcengine 二期补(占比 15.58% 贴线超阈但探测不可用,发布说明须承诺补采窗口),报告含灵敏度备注(二期重跑须重算分布表);⑥高增长 bucket 数=0(必达三家 30 天监控口径,>30% 月增速阈值)。

## Changes

### Files Created
- internal/shared/cloudx/nasprobe/oss_usage.go
- internal/shared/cloudx/nasprobe/oss_usage_test.go
- internal/shared/cloudx/nasprobe/oss_distribution_manual_test.go
- internal/shared/cloudx/aliyun/oss_probe_manual_test.go
- internal/shared/cloudx/huawei/oss_probe_manual_test.go
- internal/shared/cloudx/aws/oss_probe_manual_test.go
- internal/shared/cloudx/tencent/oss_probe_manual_test.go
- internal/shared/cloudx/volcano/oss_probe_manual_test.go
- docs/features/oss-ops-insight/probe-report.md

### Files Modified
无

### Key Decisions
- 探针落位复用 NAS 先例:共享纯逻辑进 nasprobe 包(oss_usage.go 复用 ProviderUsage/AggregateDistribution/Verdict/BytesToGB,零重复造轮子),厂商 namespace 探测进 <provider>/oss_probe_manual_test.go(external test 包,env 门控 NAS_PROBE_* 系,无 env 即 skip,生产代码零 import)
- aliyun namespace 以实盘为准定案 acs_oss_dashboard:proposal 假设的 acs_oss 实盘除旧指标 StorageUtilization 残留(403 维度不合法)外全部 400-10002 未注册,按官方文档(2026-03 更新)与实盘双证收敛;维度 BucketName、计量类 Period=3600、窗口≤31 天写入定案供 T3
- tencent 归因修正:proposal 视为「待探测」的 QCE/COS 实测订阅与注册均正常,容量单位 MB(非 byte)已列入 T4 单位换算厂商区分(byte:aliyun/huawei/aws;MB:tencent)遗留行动
- volcengine 沿用 NAS 前案方法链(真实 bucket 维度直查+静态候选矩阵+ECS 阳性对照),289 组合 metric not found 归因到订阅未开通,二期补重试路径三步固化在探测脚本注释与报告
- 分布统计双口径分列呈现:bucket 数占比为可靠口径(枚举权威),容量口径仅 aliyun/huawei 可信(GetBucketStat 快照),volcano TOS 无统计接口恒 0、aws/tencent 快照全 0 均如实标注;升格判定严格按「>15% 且探测可用」双条件,volcengine 15.58% 贴线超阈的灵敏度备注写入报告
- 报告为发布 gate:docs/features/oss-ops-insight/probe-report.md 含 namespace 定案表(T3/T4 直接引用)/非零验证汇总/分布双口径表/最终分组/遗留行动(4 项含 31 天补采窗口限制)/Hard Rule 合规声明

## Test Results
- **Tests Executed**: Yes
- **Passed**: 9
- **Failed**: 0
- **Coverage**: 39.8%

## Acceptance Criteria
- [x] 华为:OBS bucket 级容量/对象数指标实盘验证(候选 namespace SYS.OBS),≥1 个真实 bucket 非零数据点通过;文档口径与实盘不符时以实盘为准并记录
- [x] AWS:S3 BucketSizeBytes/NumberOfObjects 经 ListMetrics 确认注册 + GetMetricData 实盘非零验证(90 天窗口;未注册 bucket 零值注记+原因)
- [x] aliyun:OSS bucket 级容量指标实盘非零验证(实测定案 namespace=acs_oss_dashboard/MeteringStorageUtilization,6/6 PASS)
- [x] tencent/volcengine:探测并归因(指标订阅未开通 vs 指标不存在),记录二期补路径(tencent 探测可用修正假设;volcengine 归因订阅未开通+重试路径固化)
- [x] 分布统计:实盘 OSS bucket 数/容量按厂商占比(双口径),判定是否触发升格/降级(无升格;tencent 维持尽力而为,volcengine 二期补+发布说明承诺窗口)
- [x] 产出探测报告 docs/features/oss-ops-insight/probe-report.md(namespace 定案/分组决策/降级预案),作为 T3/T4 依据

## Notes
测试口径:testsPassed=9 为 go test 顶层用例数(nasprobe 单测 3 函数 12 子用例 + 6 个 manual probe 测试真实账号实跑全 PASS;分布/厂商探针无 env 即 skip 不计)。coverage 39.8% 为 nasprobe 包级(分母含 accounts.go Mongo 只读加载器与既有 NAS 纯逻辑),新增 oss_usage.go 三函数 100% 覆盖(coverprofile 逐函数核对),如实记录不虚报。质量门禁:go build ./... 通过;go vet ./... 通过;gofmt -s 自建文件 0 delta(仅既有 cert_lb.go 有 fmt 差异,非本任务文件);golangci-lint 未安装按 Makefile 兜底跳过。Hard Rules 全程遵守:只读凭证(List/ListMetrics/DescribeMetric*/GetMetricData/GetBucket* 类 API)、只写样例行到测试日志、不动生产表;分组由实盘探测证据支撑(报告 §3/§4)。原始探测日志 logs/oss_probe_*.txt(gitignored)。期间 forge 全局 feature 被并行会话切至 export-dialog-consolidation,按既定恢复法 forge feature set oss-ops-insight 后重试成功,未触碰并行会话 WIP。

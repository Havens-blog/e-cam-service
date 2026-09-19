---
status: "completed"
started: "2026-09-19 12:58"
completed: "2026-09-19 12:58"
time_spent: ""
---

# Task Record: 1 厂商监控 API 探测:volcengine 指标名 / 华为 namespace / 实盘非零验证 / 容量分布统计

## Summary
M1 探测定案完成:新增 nasprobe 共享探测包(单位换算/数量级自检[1MB,1PB]/分布聚合/升格判定+只读账号加载,25 个单测用例)与 4 个 env 门控 manual probe 测试(volcano cloudmonitor/huawei CES/aws CloudWatch/5 厂商容量分布),用真实账号完成全部只读探测并产出 docs/features/nas-ops-insight/probe-report.md。定案:必达=aliyun/huawei/aws,尽力而为=tencent/volcengine(二期补),无升格候选。核心发现:①华为实盘指标全部上报在 SYS.EFS/efs_instance_id(文档 namespace SYS.SFS_Turbo 实盘无任何上报,按文档实现会整表静默为空),used_capacity(byte)非零验证 5/5 PASS(jlc-fat 1378GB/56.09% 等),SC-1 华为降级预案不触发;②AWS StorageBytes 序列在 5 个 fs 全部注册但 90 天双路查询 0 数据点(实例闲置,非 API 不可用),按零值例外放行+打标呈现;③volcengine 区域网关仅注册 GetMetricData@2018-01-01,元数据接口缺失+355 候选组合含 ECS 阳性对照全 metric not found(云产品监控指标订阅未开通),二期补重试路径固化在探测脚本;④实盘分布 95 实例:aliyun 76.8%/volcengine 12.6%/huawei 5.3%/aws 5.3%/tencent 0%,双口径均 ≤15% 不升格;⑤遗留:AWS 账号 regions 配置(eu-west-1)与实盘(eu-central-1/us-east-1)不一致。

## Changes

### Files Created
- internal/shared/cloudx/nasprobe/probe.go
- internal/shared/cloudx/nasprobe/accounts.go
- internal/shared/cloudx/nasprobe/probe_test.go
- internal/shared/cloudx/nasprobe/probe_distribution_manual_test.go
- internal/shared/cloudx/volcano/probe_manual_test.go
- internal/shared/cloudx/huawei/probe_manual_test.go
- internal/shared/cloudx/aws/probe_manual_test.go
- docs/features/nas-ops-insight/probe-report.md

### Files Modified
- go.mod
- go.sum

### Key Decisions
- 探针落位 <provider>/probe_manual_test.go(external test 包)+ nasprobe 共享包,全部 env 门控(NAS_PROBE_MONGODB_DSN/CAM_ENCRYPTION_KEY/逐厂商直填),无 env 即 skip,生产代码零 import
- 华为探测遵守 Hard Rule 三 namespace 独立判定(SYS.SFS/SYS.SFS_Turbo/SYS.EFS),发现文档 namespace 与实盘上报不符后以实盘 SYS.EFS 为定案并同步写入脚本注释与报告
- volcengine 探测证据链完整化:SDK GetMetricData 候选矩阵 + Python SDK 元数据接口多版本/多网关否定 + 真实 ECS 实例 id 阳性对照,将『指标名未收敛』归因到『云产品监控指标订阅未开通』并定义重试路径,而非笼统判失败
- AWS 非零验证失败归因取证:ListMetrics 证明序列存在 + 挂载点核查 2-3 个/fs + 双 API 90 天 0 数据点 → 实例闲置,保留必达分组并以零值注记进 SC-1
- 容量分布双口径呈现(实例数占比=可靠口径,资产表容量原值占比=参考口径,单位名义化不可信),两口径对升格判定结论一致
- 期间发现并行会话跨 feature 误写(本任务 records 槽位被 cert-volcano task1 记录污染+状态误置 completed),按既定恢复法清除误写记录并核对 AUTO-RESTORE 后的 pending 状态,未触碰并行会话的 task2 in_progress 认领

## Test Results
- **Tests Executed**: Yes
- **Passed**: 9
- **Failed**: 0
- **Coverage**: 34.8%

## Acceptance Criteria
- [x] volcengine cloudmonitor 探测:真实账号跑 GetMetricData,确认 NAS 容量/使用量指标名;失败=记录错误+候选指标名+二期补判定
- [x] 华为 CES 探测:按文件系统类型分别跑 SYS.SFS 与 SYS.SFS_Turbo 两个 namespace,确认容量指标可用
- [x] 华为/AWS 实盘非零验证:对 capacity=0 实例确认监控 API 返回非零且数量级正确;零值视为探测未通过并记录原因
- [x] 实盘容量按厂商分布统计:各厂商实例数/容量占比,>15% 且探测可用标记升格候选
- [x] 产出 M1 探测报告 docs/features/nas-ops-insight/probe-report.md(定案/非零验证/分布/最终分组+升格判定)

## Notes
测试口径:testsPassed=9 为 go test 顶层用例数(nasprobe 5 个单测函数 24 子用例 + 4 个 manual probe 测试真实账号实跑全 PASS);coverage 34.8% 为 nasprobe 包级,其中纯逻辑 probe.go 五函数均 100%,分母含 accounts.go 的 Mongo 只读加载器(网络集成代码,仅 env 门控 manual 路径触达,如实记录不虚报)。Hard Rules 全程遵守:只读凭证/只写日志/不动 ecam_nas_metric(表属 T2)/华为 namespace 分开探测/不动并行会话 WIP。质量门禁:go build -p 1 ./... 通过;gofmt 自建文件 0 delta;go vet 四包通过;-race 不可用(宿主无 gcc,与既有多任务一致)。探测原始日志 logs/nas_probe_*.txt(gitignored)。

---
status: "completed"
started: "2026-09-21 16:07"
completed: "2026-09-22 10:36"
time_spent: "~18h 29m"
---

# Task Record: 1 RDS 厂商监控 API 探测:namespace/指标名实盘验证 + 内存口径归一 + 多引擎确认 + 分布定分组

## Summary
RDS 厂商监控 API 探测完成(发布 gate):5 厂商 namespace/指标名实盘验证 + 内存口径归一 + 多引擎确认 + 实盘分布统计,报告落盘 docs/features/rds-ops-insight/probe-report.md。定案:必达=aliyun/huawei/aws(实盘非零全通过 PASS=84/383/20+5);尽力而为=tencent(0 实例,QCE/CDB 206 指标已注册)、volcengine(2.08%≤15%+订阅未开通→二期补)。关键发现:huawei SYS.RDS 维度键按引擎分键(rds_cluster_id/postgresql_cluster_id);aliyun 指标名按引擎前缀分派(SQLServer_*);aws 内存换算公式 (1−FreeableMemory/Total)×100 实盘 5/5 验证;tencent 实盘注册名 CpuUseRate/MemoryUseRate 等(非文档名);aliyun 枚举快照 Storage 全 0(资产表字段未映射,登记移交项)。新增纯逻辑 nasprobe/rds_usage.go(RDSToUsage/MemoryPercentFromFreeable/MemoryPercentFromUsed)+ 单测 + 5 厂商 rds_probe_manual_test.go + rds_distribution_manual_test.go(全部 env 门控只读)。

## Changes

### Files Created
- internal/shared/cloudx/nasprobe/rds_usage.go
- internal/shared/cloudx/nasprobe/rds_usage_test.go
- internal/shared/cloudx/nasprobe/rds_distribution_manual_test.go
- internal/shared/cloudx/aliyun/rds_probe_manual_test.go
- internal/shared/cloudx/huawei/rds_probe_manual_test.go
- internal/shared/cloudx/aws/rds_probe_manual_test.go
- internal/shared/cloudx/tencent/rds_probe_manual_test.go
- internal/shared/cloudx/volcano/rds_probe_manual_test.go
- docs/features/rds-ops-insight/probe-report.md

### Files Modified
无

### Key Decisions
- 必达/尽力而为分组由实盘分布+探测可用性双因素支撑:tencent 0%/volcengine 2.08% 均 ≤15%,不触发升格,亦不触发 Disk 前案的「>15% 显式降级承诺」条款
- 内存口径归一:仅 aws 需换算(MemoryPercentFromFreeable,实盘 5/5 验证),aliyun/huawei/tencent 均 %直给;归一 0~100 走 CheckUsagePercentRange
- 多引擎结论:引擎间差异大(aliyun 指标名前缀/huawei 维度键),推翻 proposal「engine 仅元数据透传」默认假设,T3/T4 必须按 engine 分派
- huawei 内存指标以实盘为准:rds002_mem_util(文档 mem_usedPercent 未注册)
- aliyun Storage=0 为资产表既有字段缺陷,磁盘使用率采集(DiskUsage %直给)不受影响,登记移交项不阻塞

## Test Results
- **Tests Executed**: Yes
- **Passed**: 728
- **Failed**: 0
- **Coverage**: 52.8%

## Acceptance Criteria
- [x] AC-1 huawei SYS.RDS CPU/内存/磁盘/连接数实盘非零验证(≥1 真实实例)
- [x] AC-2 AWS 四指标实盘非零 + FreeableMemory 换算公式确认
- [x] AC-3 aliyun acs_rds_dashboard 四指标实盘非零验证
- [x] AC-4 tencent/volcengine 探测并归因 + 二期补路径记录
- [x] AC-5 内存口径归一 + 多引擎口径确认(差异大按 engine 分派)
- [x] AC-6 实盘分布双口径统计 + 升格/降级判定 + probe-report.md 产出

## Notes
coverage=52.8% 为 nasprobe 包整体(nasprobe 探测基础设施 LoadProbeAccounts 等为 env 门控网络代码,设计上不可单测);本任务新增纯逻辑 3 函数(RDSToUsage/MemoryPercentFromFreeable/MemoryPercentFromUsed)均为 100% 分支覆盖。testsPassed=728 含 6 个变更包既有单测(实盘 manual probe 测试无 env 时按设计 SKIP=22)。门禁:go build ./... 通过、gofmt 新文件全部干净、go vet 变更包零告警(仅 pre-existing 的 internal/logquery/service/cache_analyze_test.go 报错,与本任务无关)、golangci-lint 未安装按 Makefile 设计跳过。Hard Rule 合规:全程只读 API,未写任何生产数据;实盘探测日志 logs/rds_probe_*.txt(gitignored)。make lint 因 Makefile 自身 bash 引号问题在 Git Bash 下报错(pre-existing,Makefile 设计为 golangci-lint 缺失时跳过)。

---
feature: "nas-ops-insight"
created: "2026-09-19"
status: tasks
mode: quick
---

# Feature (Quick): nas-ops-insight

<!-- Status flow: tasks -> in-progress -> completed -->

## Documents

| Document | Path |
|----------|------|
| Proposal | ../../proposals/nas-ops-insight/proposal.md |

## Tasks

| ID | Title | Status | File |
|----|-------|--------|------|
| 1 | 厂商监控 API 探测:volcengine 指标名 / 华为 namespace / 实盘非零验证 / 容量分布统计 | pending | 1-probe-vendors.md |
| 2 | NASMetric 模型 + NASMetricQuerier 接口 + ecam_nas_metric DAO 建表 | pending | 2-model-dao-schema.md |
| 3 | 必达厂商 NAS 监控适配器(aliyun/huawei/aws) | pending | 3-mandatory-adapters.md |
| 4 | 尽力而为厂商 NAS 监控适配器(tencent/volcengine)+ 单位归一化工具 | pending | 4-best-effort-adapters.md |
| 5 | nas:collect_metrics 采集执行器(账号遍历 + 首写生效 + 注册) | pending | 5-collect-executor.md |
| 6 | 采集失败可观测 + 自我健康监控(失败计数入 Result + 连续零成功告警) | pending | 6-observability-health.md |
| 7 | 持久化日闸 + 原子认领(scheduler_state, NAS 采用) | pending | 7-persistent-gate.md |
| 8 | CDN 日闸迁移到持久化 + 值回归 + 特性开关回滚 | pending | 8-cdn-migration-rollback.md |
| 9 | 一次性历史回填任务(配额节流 + 错峰 + 幂等去重) | pending | 9-backfill.md |
| 10 | NAS 指标读取接口:单实例趋势 + Top(租户校验 + 分页 + qc_status 闭环) | pending | 10-read-apis.md |
| 11 | 前端:NAS 抽屉监控 tab 趋势图 + 列表页运营卡(空态/警示/数据来源统一) | pending | 11-frontend.md |

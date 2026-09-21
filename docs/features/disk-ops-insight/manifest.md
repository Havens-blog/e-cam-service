---
feature: "disk-ops-insight"
created: "2026-09-21"
status: tasks
mode: quick
---

# Feature (Quick): disk-ops-insight

<!-- Status flow: tasks -> in-progress -> completed -->

## Documents

| Document | Path |
|----------|------|
| Proposal | ../../proposals/disk-ops-insight/proposal.md |

## Tasks

| ID | Title | Status | File |
|----|-------|--------|------|
| 1 | Disk 厂商监控 API 探测:namespace/指标名实盘验证 + 使用率口径归一 + 分布定分组 | pending | 1-probe-vendors.md |
| 2 | DiskMetric 模型 + DiskMetricQuerier 接口 + ecam_disk_metric DAO 建表 | pending | 2-model-dao-schema.md |
| 3 | 必达厂商 Disk 监控适配器(aliyun/huawei/aws) | pending | 3-mandatory-adapters.md |
| 4 | 尽力而为厂商 Disk 监控适配器(tencent/volcengine) | pending | 4-best-effort-adapters.md |
| 5 | disk:collect_metrics 采集执行器(账号遍历 + 首写生效 + 注册) | pending | 5-collect-executor.md |
| 6 | 采集失败可观测 + 自我健康监控(失败计数入 Result + 连续零成功告警) | pending | 6-observability-health.md |
| 7 | scheduler_state 日闸 disk 键接入 + 采集任务注册 | pending | 7-persistent-gate-disk.md |
| 8 | Disk 指标读取接口:单盘趋势 + Top(租户校验 + 分页 + qc_status 闭环) | pending | 8-read-apis.md |
| 9 | 前端:Disk 抽屉监控 tab 趋势图 + 列表页运营卡(空态/警示/数据来源统一) | pending | 9-frontend.md |

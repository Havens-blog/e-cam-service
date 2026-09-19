---
id: "9"
title: "一次性历史回填任务(配额节流 + 错峰 + 幂等去重)"
priority: "P1"
estimated_time: "1.5d"
complexity: "medium"
dependencies: [5]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 9: 一次性历史回填任务(配额节流 + 错峰 + 幂等去重)

## Description

上线时从厂商监控 API 拉取启用日前 N 天(14~90 天)历史指标,让 30 天趋势上线即可见,避免运营等 N 天才有趋势。按「厂商 × 账号」分片批处理,配额节流(批 ≤5 实例 × ≤10 天、批间退避、与每日采集错峰),已成功批次以唯一键幂等去重。

## Reference Files
- `docs/proposals/nas-ops-insight/proposal.md` — 日快照取值口径与采集窗口(回填配额节流)、Next Steps、Urgency
- `internal/cam/task/executor/sync_cdn_metrics.go`: 执行器批量采集参照(分片/进度)
- `internal/cam/task/executor/sync_nas.go`: NAS 实例遍历参照
- `docs/features/nas-ops-insight/tasks/3-vendor-adapters.md`: 适配器接口(本任务复用)

## Acceptance Criteria
- [ ] 回填任务(如 `nas:backfill_metrics`)从厂商 API 拉取启用日前 N 天(14~90 天,默认 30 天)历史,写入 `ecam_nas_metric`
- [ ] 配额节流:按「厂商 × 账号」分片;批大小默认 ≤5 实例 × 连续 ≤10 天;批间退避默认 5 秒起,遇限流指数退避至上限;CloudWatch GetMetricData 配额换算留 30% 余量
- [ ] 错峰:回填在 01:30~06:00 窗口执行,与每日自动采集(00:10 后)不碰撞;命中限流的厂商回填挂起、次日窗口续跑
- [ ] 幂等去重:已成功批次不重试(以 `(account_id, fs_id, date)` 唯一键幂等),重跑不产生脏行
- [ ] 单测:分片参数、退避逻辑、幂等重跑、错峰窗口判定
- [ ] `go build ./...` 通过

## Hard Rules
- 回填批大小/退避参数必须落地为配置常量(非硬编码散落)
- 已成功批次不得重试(唯一键幂等)

## Implementation Notes
- 回填复用 T4 采集执行器的适配器调用与 DAO 写入;差异在:多天批量 + 节流分片 + 错峰窗口。
- 参照 `internal/cam/task/executor/sync_cdn_metrics.go` 的账号遍历与进度更新模式。
- 风险提示(proposal 日快照节):CloudWatch 有 GetMetricData 配额(每 60 秒 5 万指标点),批大小按配额换算;命中限流挂起次日续跑。

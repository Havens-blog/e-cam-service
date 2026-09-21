---
journey: "disk-day-gate-resilience"
step: 2
step-action: "采集成功后提交日闸"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/disk-day-gate-resilience/journey.md
skip_eval: true
---

# Contract: disk-day-gate-resilience / Step 2: 采集成功后提交日闸

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "当日 disk 采集与落库已完成,日闸认领成功"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "DiskMetric"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "执行器提交当日日闸状态"
- Output: "日闸记录当日已完成状态;当日后续分钟级调度检查全部跳过,不再重复采集"
- State: "scheduler_state disk 键当日记录为已完成/已认领"
- Side-effect: "none"

## Outcome "read-failure-backoff"
- Preconditions: "读取 scheduler_state 状态时存储不可用"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "持久化日闸底层存储读取故障"
        prerequisite_entity: "CloudAccount"
- Input: "执行器读取日闸状态以决定是否认领"
- Output: "进入 ≥5 分钟退避窗口,不热循环刷存储;恢复后按正常语义继续认领"
- State: "无部分写入,当日事实不被破坏"
- Side-effect: "退避等待"

## Outcome "submit-failure-no-rollback"
<!-- source: inferred -->
<!-- reasoning: Fact Table DISK_GATE_SUBMIT_FAIL(auto_sync_disk_metrics.go:78-86):认领已持久化后队列 Submit 失败(队列关闭/打满)仅 ERROR 留痕,不回滚认领——回滚会与多副本认领竞争,属运维级故障边界 -->
- Preconditions: "当日认领已持久化,但任务队列 Submit 失败(队列关闭/打满)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "scheduler_state disk 键当日已认领,任务未入队"
        prerequisite_entity: "CloudAccount"
- Input: "执行器向任务队列提交采集任务失败"
- Output: "记录 ERROR 日志留痕(task_id/date);不回滚当日认领,今日不再重试(避免与多副本认领竞争)"
- State: "当日认领保持已认领;无采集任务创建"
- Side-effect: "none"

## Journey Invariants
- 原子性:任意并发度下,同一 resource_type 键同一日的认领结果恰好一个胜出
- 持久化一致性:服务重启不改变「当日已认领/已完成」事实,重启 ×3 各仅 1 条
- 故障不静默:写失败/读失败必须走退避与告警路径,禁止热循环与静默吞错
- 既有键零回归:disk 键的接入与回滚不得改变 nas/cdn/oss 键的任何既有行为

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "CloudAccount"
      min_count: 1
    - entity_type: "DiskMetric"
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "CloudAccount"
  state_requirements:
    - description: "success 分支要求采集已完成可提交;read-failure 分支要求存储读故障;submit-failure 分支要求队列故障且认领已持久化"
      prerequisite_entity: "CloudAccount"
```

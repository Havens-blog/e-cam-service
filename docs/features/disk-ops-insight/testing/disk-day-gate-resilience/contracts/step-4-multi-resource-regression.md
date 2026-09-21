---
journey: "disk-day-gate-resilience"
step: 4
step-action: "四资源日闸并行复用"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/disk-day-gate-resilience/journey.md
skip_eval: true
---

# Contract: disk-day-gate-resilience / Step 4: 四资源日闸并行复用

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "CDN/NAS/OSS/Disk 四资源的每日调度在共享日闸机制下并行运行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "scheduler_state 中 nas/cdn/oss/disk 各 resource_type 键并存可用"
        prerequisite_entity: "CloudAccount"
- Input: "观察各资源每日调度的认领与提交"
- Output: "各资源按各自 resource_type 键独立认领/提交,互不干扰;Disk 分支接入后 NAS/CDN/OSS 既有调度行为不变"
- State: "scheduler_state 各资源键各自更新各自的 last_date"
- Side-effect: "各资源采集任务各自入队"

## Outcome "existing-keys-regression"
- Preconditions: "disk 键首次接入 scheduler_state,既有 nas/cdn/oss 键已有历史状态记录"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "nas/cdn/oss 键存在历史 last_date 记录,disk 键尚无记录"
        prerequisite_entity: "CloudAccount"
- Input: "回归验证三既有键的认领/提交/重启语义"
- Output: "nas/cdn/oss 键行为与 Disk 接入前完全一致:按 resource_type 分键,无键冲突、无状态串扰"
- State: "既有键历史记录保持不变"
- Side-effect: "none"

## Outcome "disk-key-isolation"
<!-- source: inferred -->
<!-- reasoning: Fact Table DISK_DAY_GATE_CLAIM(daily_gate.go:TryClaim 条件写 resource_type=disk AND last_date<today):所有认领条件写均限定 resource_type,任一键的退避/失败状态不跨键传播——键隔离的写路径依据 -->
- Preconditions: "disk 键处于当日认领/退避/写失败重试状态期间,nas/cdn/oss 键正常可用"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "disk 键存在退避或失败状态,nas/cdn/oss 键当日未认领"
        prerequisite_entity: "CloudAccount"
- Input: "在 disk 键故障期间并行触发 nas/cdn/oss 的每日调度"
- Output: "nas/cdn/oss 键的当日认领与提交不受 disk 键任何退避或失败状态影响,照常胜出并提交"
- State: "键间零串扰,各键当日事实独立"
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
  state_requirements:
    - description: "success 分支要求四资源键并存;regression 分支要求既有键有历史记录;isolation 分支要求 disk 键故障态与既有键正常态并存"
      prerequisite_entity: "CloudAccount"
```

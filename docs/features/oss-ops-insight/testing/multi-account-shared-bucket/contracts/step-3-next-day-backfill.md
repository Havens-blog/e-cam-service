---
journey: "multi-account-shared-bucket"
step: 3
step-action: "次日补采覆盖昨日行"
generated: "2026-09-20"
skip_eval: true
sources:
  - docs/features/oss-ops-insight/testing/multi-account-shared-bucket/journey.md
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: multi-account-shared-bucket / Step 3: 次日补采覆盖昨日行

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "per-account-yesterday-overwritten"
- Preconditions: "次日采集执行;三账号的 shared-assets 昨日行均存在且为凌晨初态"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 3
      - entity_type: "OSSMetricRow"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "date"
            value: "昨日"
- Input: "次日采集包含昨日区间,对三账号的昨日行执行覆盖更新"
- Output: "各账号的昨日行被更新为厂商当日最终聚合值;写入成功"
- State: "三账号昨日行均为日末态;今日行独立不受影响"
- Side-effect: "none"
- Invariants: "补采覆盖后昨日行冻结,跨日不可变"

## Journey Invariants

- 今日行首写生效、昨日行补采后冻结,两窗口语义不互相渗透
- 唯一键 (account_id, bucket_name, date) 全程有效,覆盖更新不产生跨账号串行

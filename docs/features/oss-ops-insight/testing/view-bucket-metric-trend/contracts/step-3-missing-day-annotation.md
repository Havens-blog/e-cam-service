---
journey: "view-bucket-metric-trend"
step: 3
step-action: "缺失日以 data_status 标注"
generated: "2026-09-20"
skip_eval: true
sources:
  - docs/features/oss-ops-insight/testing/view-bucket-metric-trend/journey.md
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: view-bucket-metric-trend / Step 3: 缺失日以 data_status 标注

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "missing-day-annotated-no-fake-value"
- Preconditions: "用户已通过鉴权;窗口内存在采集断档(部分日期在指标表无行)"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 2
        field_constraints:
          - field: "date"
            value: "窗口内不连续(存在缺失日)"
- Input: "用户查看窗口内存在采集断档的日期点"
- Output: "缺失日通过 data_status=missing 显式标注,storage_size/object_count 为 null(缺失即缺失);不填假值(不用 0 或邻近值冒充);已有日正常返回"
- State: "无状态变化(纯读)"
- Side-effect: "none"
- Invariants: "缺失日只做标注,绝不填假值"

## Journey Invariants

- 缺失日只做 data_status 标注,绝不填假值(0/邻近值冒充)

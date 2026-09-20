---
journey: "view-bucket-metric-trend"
step: 4
step-action: "zero_exception 行原样暴露"
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

# Contract: view-bucket-metric-trend / Step 4: zero_exception 行原样暴露

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "zero-exception-mapped-to-data-status"
- Preconditions: "用户已通过鉴权;窗口内存在 qc_status=zero_exception(storage_size=0 异常)的指标行"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 1
        field_constraints:
          - field: "storage_size"
            value: 0
          - field: "qc_status"
            value: "zero_exception"
- Input: "用户查看该行在趋势中的呈现"
- Output: "读取响应原样暴露 qc_status 并映射 data_status=zero_exception;前端可分辨「容量为 0 是异常」而非当正常空桶;该日 storage_size 原样为 0"
- State: "无状态变化(纯读)"
- Side-effect: "none"
- Invariants: "qc_status 写入态在读取响应中原样可辨,异常零与正常空桶可区分"

## Outcome "zero-capacity-derivation-null"
<!-- source: inferred -->
<!-- reasoning: Journey Step 4b 定义容量为 0 时使用率派生不 panic;Fact Table OSS_UTILIZATION_NOT_STORED——utilization 读取侧派生,容量 0 须为 null 而非除零 panic/NaN -->
- Preconditions: "某行 storage_size=0;读取侧需要派生使用率类数值"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 1
        field_constraints:
          - field: "storage_size"
            value: 0
- Input: "请求包含该行的趋势/统计响应"
- Output: "used/capacity 边界处理:容量为 0 时使用率派生为 null 而非除零 panic/NaN;接口正常返回"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Journey Invariants

- qc_status 写入态在读取响应中原样可辨,异常零与正常空桶可区分
- 容量为 0 时使用率派生为 null,绝不除零 panic/NaN

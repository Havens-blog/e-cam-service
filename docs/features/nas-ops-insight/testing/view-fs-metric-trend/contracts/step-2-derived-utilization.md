---
journey: "view-fs-metric-trend"
step: 2
step-action: "解读派生使用率与数据状态"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/view-fs-metric-trend/journey.md
---

# Contract: view-fs-metric-trend / Step 2: 解读派生使用率与数据状态

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "窗口内目标 fs 的指标行均为正常非零行(capacity 大于 0 且不小于 used)"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 2
        field_constraints:
          - field: "capacity"
            value: "大于 0"
          - field: "used_capacity"
            value: "介于 0 与 capacity 之间"
- Input: "运营查看趋势中 utilization 曲线与 data_status/qc_status 标注"
- Output: "utilization 由 capacity/used 读取时派生(非独立落库字段),取值 0~1;used=0 且 capacity 大于 0 时 utilization=0;data_status=ok 且 qc_status 原样透出"
- State: "无状态变更(纯读派生)"
- Side-effect: "none"

## Outcome "missing-day-annotated"
- Preconditions: "回看窗口内某几日无指标行(采集未启用日/厂商缺口)"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 1
        field_constraints:
          - field: "date"
            value: "窗口内仅部分日期有行,存在缺失日"
- Input: "请求趋势接口并检查缺失日"
- Output: "缺失日以 data_status=missing 标注,capacity/used/utilization 均为 null,不填充 0 或假值;有数据日返回真实值"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "zero-exception-null-utilization"
- Preconditions: "某日行 qc_status=zero_exception(capacity=0)"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 1
        field_constraints:
          - field: "capacity"
            value: "0"
          - field: "qc_status"
            value: "zero_exception"
- Input: "请求趋势接口并检查该行"
- Output: "该行原样暴露 qc_status 并映射 data_status=zero_exception,utilization=null;前端渲染警示/异常标记而非当作正常零容量"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants

- utilization 永远由 capacity/used 现场派生,响应不出现 NaN;used>capacity 时按 min(used, capacity) 收敛参与计算且原始 used 仍返回
- 缺失日只以 data_status=missing 标注,绝不用 0/假值填充
- zero_exception 行 utilization 为 null 且可分辨

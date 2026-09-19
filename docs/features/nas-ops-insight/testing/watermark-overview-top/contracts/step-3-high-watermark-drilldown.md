---
journey: "watermark-overview-top"
step: 3
step-action: "识别高水位实例"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/watermark-overview-top/journey.md
---

# Contract: watermark-overview-top / Step 3: 识别高水位实例

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "运营已获得 Step 1/Step 2 的运营卡与 Top 数据;排行中存在高水位实例(均值使用率靠前且非 zero_exception 异常行);该实例的 fs_id 可用于趋势下钻"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 2
        field_constraints:
          - field: "used_capacity"
            value: "各行互不相同且至少一行接近 capacity(高水位)"
- Input: "运营依据 Top 排行与运营卡定位高水位实例并下钻抽屉趋势"
- Output: "排行反映真实水位;共享 fs 不因多账号并存而双计失真;高水位实例可凭 fs_id 下钻到抽屉趋势(Step 1 趋势契约)"
- State: "无状态变更(纯决策步骤,无接口调用)"
- Side-effect: "none"

## Journey Invariants

- 排行与聚合口径以「日期 desc 再容量 desc」第一行为准,共享 fs 永不双计
- 高水位判定基于近 N 天均值口径的 utilization,zero_exception 行单独可见不混入正常排行语义

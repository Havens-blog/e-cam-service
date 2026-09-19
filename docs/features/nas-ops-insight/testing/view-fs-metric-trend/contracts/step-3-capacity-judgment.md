---
journey: "view-fs-metric-trend"
step: 3
step-action: "判断扩容时机"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/view-fs-metric-trend/journey.md
---

# Contract: view-fs-metric-trend / Step 3: 判断扩容时机

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "运营已获得 Step 1/Step 2 的完整趋势数据(升序日值序列+data_status 标注);窗口内至少一行正常非零行可识别峰值"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 2
        field_constraints:
          - field: "used_capacity"
            value: "各行互不相同(可分辨最高水位日)"
- Input: "运营基于近 30 天水位(日值序列最大值即近 N 天峰值口径)判断扩容时机"
- Output: "趋势数据足以表达「近 N 天里水位最高的那一天」;不承诺日内尖峰(日末态快照架构一致)"
- State: "无状态变更(纯决策步骤,无接口调用)"
- Side-effect: "none"

## Journey Invariants

- 趋势数据以 Asia/Shanghai 自然日日末态快照表达水位,峰值口径为「日值序列最大值」
- 界面趋势数据仅来自 `ecam_nas_metric` 指标表

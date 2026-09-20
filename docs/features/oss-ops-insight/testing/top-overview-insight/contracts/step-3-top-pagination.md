---
journey: "top-overview-insight"
step: 3
step-action: "分页浏览大结果集"
generated: "2026-09-20"
skip_eval: true
sources:
  - docs/features/oss-ops-insight/testing/top-overview-insight/journey.md
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: top-overview-insight / Step 3: 分页浏览大结果集

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "pagination-seamless"
- Preconditions: "用户已通过鉴权;租户下合计有足以分页的 bucket(如 15 个以上含指标数据)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
      - entity_type: "OSSMetricRow"
        min_count: 15
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "用户以 page=2&page_size=10 请求 Top 列表第二页"
- Output: "返回第二页数据及分页元信息(total/page/page_size);翻页结果与第一页无缝衔接、不重不漏"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Outcome "page-out-of-range-empty"
<!-- source: inferred -->
<!-- reasoning: Journey Step 3b 定义页码超界;Fact Table OSS_TOP_RESPONSE(service/asset_oss_query.go:253-266)页码超界返回空 items 且 total 不变,不报 500 -->
- Preconditions: "结果共 3 页;用户已通过鉴权"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 15
- Input: "请求 Top 接口 page=99"
- Output: "返回空数据页与正确分页元信息(total 不变);不报 500、不重复返回已有页数据"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Journey Invariants

- 分页元信息(total/page/page_size)始终与去重后结果集一致
- 页码超界返回空页而非错误,不重复返回已有页数据

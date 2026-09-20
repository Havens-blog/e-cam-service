---
journey: "top-overview-insight"
step: 2
step-action: "切换排序维度为对象数"
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

# Contract: top-overview-insight / Step 2: 切换排序维度为对象数

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- density override: invalid-sort(1d) 与 top-over-limit(2b) 合并为单一 disjunctive Outcome invalid-query-parameter,以满足 Low 风险密度上限 -->

## Outcome "top-sorted-by-avg-object-count"
- Preconditions: "用户已通过鉴权;租户下有含指标数据的 bucket,object_count 互有差异"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "OSSMetricRow"
        min_count: 5
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "object_count"
            value: "各 bucket 互有差异"
- Input: "用户以 sort=object_count 请求 Top 列表"
- Output: "返回按对象数近 N 天均值口径排序的 Top 列表;排序口径与容量排序一致(均值语义);bucket_name 去重"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Outcome "invalid-query-parameter"
<!-- source: inferred -->
<!-- reasoning: Journey Step 1d(sort=cost 非法枚举)与 Step 2b(top=100 超上限 50)合并;Fact Table OSS_TOP_PARAMS(asset_handler_oss_metrics.go:66-82)——sort 非法值直接 400,top/page_size 由服务端 normalize 收敛 -->
- Preconditions: "用户已通过鉴权;客户端传入非法参数——sort 不在 storage_size|object_count 枚举内,或 top 超过最大 50(disjunctive: 任一参数越界即命中)"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 1
- Input: "请求 Top 接口并携带非法 sort 值或超上限的 top 值"
- Output: "非法 sort 返回 400 类响应并明确提示合法 sort 枚举;超上限 top 被服务端按上限收敛(默认 10 最大 50 语义生效),不返回未受限结果集"
- State: "无状态变化(参数校验/收敛)"
- Side-effect: "none"

## Journey Invariants

- `sort` 仅支持 storage_size|object_count,非法值校验拒绝
- `top` 默认 10 最大 50,参数边界在服务端强制

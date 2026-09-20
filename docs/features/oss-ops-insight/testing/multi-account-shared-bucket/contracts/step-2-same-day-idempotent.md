---
journey: "multi-account-shared-bucket"
step: 2
step-action: "同日幂等(首写生效)"
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

# Contract: multi-account-shared-bucket / Step 2: 同日幂等(首写生效)

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "first-write-kept-per-account"
- Preconditions: "三账号的 shared-assets 今日行均已存在(首写完成)"
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
            value: "今日"
          - field: "bucket_name"
            value: "shared-assets"
- Input: "同日再次触发采集,对同 (account_id, bucket_name, 今日) 键重复 upsert"
- Output: "各账号行保持首写值不被覆盖(首写生效仅保护今日行);仅补当日缺失行;无错误"
- State: "三账号今日行内容不变"
- Side-effect: "none"

## Outcome "cross-account-aggregation-guard"
<!-- source: inferred -->
<!-- reasoning: Journey Step 2b 定义跨账号误聚合回归守护;Fact Table OSS_TOP_DEDUP/OSS_ASSET_SNAPSHOT_EXCLUDED——聚合按唯一键隔离行各自统计,同名 bucket 不叠加多账号数值 -->
- Preconditions: "三账号 shared-assets 各有指标行(数值各不相同);聚合读取逻辑被执行"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 3
        field_constraints:
          - field: "date"
            value: "今日"
          - field: "bucket_name"
            value: "shared-assets"
          - field: "storage_size"
            value: "三账号互不相同"
- Input: "请求 Top/运营卡汇总"
- Output: "禁止跨账号求和/双计——总容量按唯一键隔离行各自统计,同名 bucket 不叠加多账号数值"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Journey Invariants

- Top/聚合视图绝不跨账号求和/双计
- 今日行首写生效,同日重跑不覆盖

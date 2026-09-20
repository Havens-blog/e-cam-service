---
journey: "oss-metric-insight-lifecycle"
step: 4
step-action: "用户在 OSS 列表页查看运营卡"
generated: "2026-09-20"
skip_eval: true
sources:
  - docs/features/oss-ops-insight/testing/oss-metric-insight-lifecycle/journey.md
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: oss-metric-insight-lifecycle / Step 4: 用户在 OSS 列表页查看运营卡

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "ops-card-from-metric-table"
- Preconditions: "用户已通过鉴权(合法租户凭证);该租户下指标表已有当日采集落库的指标行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Tenant"
      - entity_type: "OSSMetricRow"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "date"
            value: "含今日在内的多个自然日"
- Input: "用户打开 OSS 列表页,请求顶部运营卡的总容量/对象数/近 7 天增速"
- Output: "运营卡数值全部来自 ecam_oss_metric 指标表聚合(总容量/对象数/近 7 天增速);不出现资产表 ecam_instance 快照数值;数据齐备时正常展示,不出现「资产表快照 vs 指标表趋势」同屏矛盾"
- State: "无状态变化(纯读)"
- Side-effect: "none"
- Invariants: "OSS 界面一律以指标表为唯一数据来源"

## Outcome "unauthorized"
<!-- surface-required: API 表面要求——认证端点必须派生 unauthorized 结果 -->
- Preconditions: "请求未携带有效凭证(或凭证过期)"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 1
- Input: "无凭证请求 OSS 列表页的运营卡聚合接口"
- Output: "401 Unauthorized;响应体含认证错误信息,不泄露敏感数据"
- State: "无状态变化(请求被中间件拦截,未进入业务查询)"
- Side-effect: "none"

## Outcome "snapshot-metric-mismatch"
<!-- source: inferred -->
<!-- reasoning: Journey Step 6b 定义资产表快照与指标表不一致;Fact Table OSS_ASSET_SNAPSHOT_EXCLUDED 保障界面唯一数据来源为指标表 -->
- Preconditions: "资产表 ecam_instance 的 storage_size 快照与 ecam_oss_metric 最新指标值不一致(资产同步滞后);用户已通过鉴权"
  fixture_spec:
    entities:
      - entity_type: "OSSBucketAsset"
        min_count: 1
        field_constraints:
          - field: "storage_size"
            value: "与指标表最新值不同的过期快照值"
      - entity_type: "OSSMetricRow"
        min_count: 1
        field_constraints:
          - field: "date"
            value: "今日"
- Input: "用户查看 OSS 列表行/运营卡/趋势图的容量数值"
- Output: "OSS 界面一律显示指标表数据;资产表快照数值不出现在 OSS 界面(资产表仅作 bucket 枚举与元数据)"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Journey Invariants

- OSS 相关界面(列表行容量/对象数、运营卡、趋势图、Top)一律以 ecam_oss_metric 指标表为唯一数据来源
- 认证端点未授权访问一律 401,不泄露业务数据

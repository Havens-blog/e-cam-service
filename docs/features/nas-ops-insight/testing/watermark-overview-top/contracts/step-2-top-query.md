---
journey: "watermark-overview-top"
step: 2
step-action: "查看 Top 排行"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/watermark-overview-top/journey.md
---

# Contract: watermark-overview-top / Step 2: 查看 Top 排行

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "运营已登录且持有租户资产查看权限;租户账号近 N 天存在指标行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "等于当前会话租户"
      - entity_type: "NASMetric"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "GET /assets/nas/top?account_id=&days=&sort=utilization&top=10&page=1&page_size=10"
- Output: "200 与 { total, page, page_size, items[] };items 按 sort 字段(近 N 天均值口径)降序取前 N(默认 10,最大 50);每条含 fs_id/fs_name/account_id 列表/data_status/qc_status 与最新一天、近 N 天均值两类 capacity·used·utilization"
- State: "无状态变更(纯读)"
- Side-effect: "none"

## Outcome "params-invalid-400"
- Preconditions: "运营已通过鉴权;请求 top=51 或 page_size=51(超最大 50),或 days 越出 1~90,或 sort 传非法值"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "GET /assets/nas/top?top=51(或 page_size=51、days=91、sort=非法值)"
- Output: "400 参数校验失败并列出失败项;sort 合法域为 capacity|utilization;top/page_size 上限 50"
- State: "无状态变更;不触发聚合查询"
- Side-effect: "none"

## Outcome "cross-tenant-account-404"
- Preconditions: "运营已通过鉴权;请求携带非本租户的 account_id"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "tenant_id"
            value: "分属两个租户"
- Input: "GET /assets/nas/top?account_id=租户外账号"
- Output: "404 不泄露账号存在性"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "unauthorized-401"
- Preconditions: "客户端未携带有效鉴权请求 Top 端点"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "无有效凭据的 GET /assets/nas/top 请求"
- Output: "401 未认证响应"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "empty-collection-pagination"
- Preconditions: "账号下无任何 NAS 指标行,或 page 超出总页数"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "等于当前会话租户"
    state_requirements:
      - description: "该账号范围近 N 天内 ecam_nas_metric 无行(或请求页码超出 total 对应页数)"
        prerequisite_entity: "NASMetric"
- Input: "GET /assets/nas/top?account_id=无指标账号 或 page=超出页码"
- Output: "200 与空 items[] 及正确 total/page/page_size 元数据,不报错"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants

- 任何聚合视图必须先按 fs_id 去重再计数,共享 fs 永不双计
- Top 的 sort 仅接受 capacity|utilization,utilization 一律取近 N 天均值口径
- 越权 account_id 一律 404 且不泄露账号存在性;未认证一律 401
- 空集合/超页返回 200 空页与正确分页元数据,不报错

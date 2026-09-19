---
journey: "multi-account-shared-fs"
step: 3
step-action: "趋势按账号隔离读取"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/multi-account-shared-fs/journey.md
---

# Contract: multi-account-shared-fs / Step 3: 趋势按账号隔离读取

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "运营持有租户鉴权上下文;同一 fs_id 被租户内三个账号采集且各有指标行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 3
        field_constraints:
          - field: "tenant_id"
            value: "等于当前会话租户"
      - entity_type: "NASMetric"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "fs_id"
            value: "相同 fs_id"
- Input: "分别以三个 account_id 请求 GET /assets/nas/metrics?fs_id=同一fs&account_id=各账号&days=30"
- Output: "各账号返回各自视角的日值序列(趋势按账号保留各自行),三次请求均 200 且鉴权校验各自通过;序列数值互不混合"
- State: "无状态变更(纯读)"
- Side-effect: "none"

## Outcome "cross-tenant-account-404"
- Preconditions: "运营仅持有账号 1 所在租户的权限,尝试以账号 2(不属于本租户)的 account_id 读同 fs 趋势"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "tenant_id"
            value: "分属不同租户"
      - entity_type: "NASMetric"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "GET /assets/nas/metrics?fs_id=&account_id=租户外账号"
- Output: "404 响应,不泄露账号 2 的存在性与指标数据"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "unauthorized-401"
- Preconditions: "客户端未携带有效登录会话请求趋势端点"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "无有效凭据的 GET /assets/nas/metrics 请求"
- Output: "401 未认证响应,不泄露任何指标数据"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants

- 趋势读取按账号隔离,聚合视图按 fs_id 去重——两条路径的隔离/去重语义互不混淆
- 越权 account_id 一律 404 且不泄露账号存在性;未认证一律 401

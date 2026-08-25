---
journey: "no-snapshot-guidance"
step: 4
step-action: "快照 done 后进入预览"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/no-snapshot-guidance/journey.md
skip_eval: true
---

# Contract: no-snapshot-guidance / Step 4: 快照 done 后进入预览

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "轮询观测到最近快照状态变为 done 且引用已落库；操作者具备 OpsEngineer 角色"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
      - entity_type: "CertReference"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "ScanSnapshot"
- Input: "运维人员在快照 done 后请求 GET /api/v1/certs/discovery/preview 进入预览列表"
- Output: "预览基于该 done 快照正常生成唯一证书清单（三元组去重、七类字段、快照时点），引导流程闭环，进入预览-确认流程"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "partial-success-still-usable"
- Preconditions: "快照收敛到 done 但部分云账号扫描失败（partialFailures 非空），成功账号的引用已落库"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
          - field: "partialFailures"
            value: "非空"
      - entity_type: "CertReference"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "ScanSnapshot"
        field_constraints:
          - field: "cloud"
            value: "来自扫描成功的账号"
- Input: "运维人员进入预览列表"
- Output: "预览基于已成功落库的引用正常生成清单（不因 partialFailures 整体失败）；partialFailures 信息可经快照状态端点查询，运维人员可判断是否需要重扫补齐"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效登录会话（未认证）"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
    state_requirements:
      - description: "请求方未认证"
        prerequisite_entity: "ScanSnapshot"
- Input: "不带有效凭证请求发现预览"
- Output: "401 语义的认证失败响应，不返回预览清单"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- NO_SNAPSHOT 为结构化错误码，快照 done 后预览必须可正常生成
- 快照状态端点与预览端点同权限口径

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ScanSnapshot"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "done"
    - entity_type: "CertReference"
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "ScanSnapshot"
  state_requirements:
    - description: "partial-success 分支 partialFailures 非空但仍有成功落库引用"
      prerequisite_entity: "ScanSnapshot"
```

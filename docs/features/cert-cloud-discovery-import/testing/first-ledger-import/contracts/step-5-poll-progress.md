---
journey: "first-ledger-import"
step: 5
step-action: "轮询进度至终态"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/first-ledger-import/journey.md
skip_eval: true
---

# Contract: first-ledger-import / Step 5: 轮询进度至终态

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "导入会话已收敛到终态（completed 或 partial_failed），存在 finishedAt"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "completed 或 partial_failed"
          - field: "items"
            value: "全部条目有终态结果"
- Input: "以 OpsEngineer 身份 GET /api/v1/certs/discovery/import/:sessionId 轮询会话进度"
- Output: "200 响应与会话创建同构：status 为终态、finishedAt 有值、progress 的 total/succeeded/failed 计数一致、失败条目携带静态 errorReason 文案可见"
- State: "无状态变更（只读轮询）"
- Side-effect: "none"

## Outcome "browser-reopen-resume"
- Preconditions: "导入会话执行中（running，部分条目已有结果），浏览器曾被关闭或刷新"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
          - field: "progress"
            value: "succeeded+failed 大于 0 且小于 total"
- Input: "运维人员重新打开台账页与导入入口后轮询进度端点"
- Output: "进度可续看：返回已持久化的会话与已处理条目结果，不丢结果"
- State: "无状态变更（会话在创建时已先持久化，异步执行与浏览器生命周期无关）"
- Side-effect: "none"

## Outcome "session-timeout-retryable"
- Preconditions: "勾选条目数量大，逐证限速调用云 API 使会话逼近整体限时（10 分钟口径）仍未完成"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
          - field: "progress"
            value: "total 较大且处理耗时逼近整体限时"
    state_requirements:
      - description: "会话整体限时到期后剩余条目不再调云 API，逐条记超时失败因"
        prerequisite_entity: "DiscoveryImportSession"
- Input: "运维人员持续轮询会话进度直至终态"
- Output: "超时条目记 SESSION_TIMEOUT 可重跑语义的失败因；会话分批记录进度并最终收敛终态，不静默丢失已处理结果"
- State: "已处理条目结果保留；未处理条目标记超时失败"
- Side-effect: "none"

## Outcome "session-not-found"
<!-- source: inferred -->
<!-- reasoning: Fact Table PROGRESS_NOT_FOUND（response.go:126-135, discovery_handler.go:207-214）：GetSession 命中 mongo.ErrNoDocuments 时 WriteError 映射 404 CERT_NOT_FOUND；API surface 常见 not-found 边界 -->
- Preconditions: "请求的 sessionId 不存在任何对应会话"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "不存在与 sessionId 对应的 DiscoveryImportSession"
        prerequisite_entity: "DiscoveryImportSession"
- Input: "以 OpsEngineer 身份 GET /api/v1/certs/discovery/import/:sessionId，sessionId 为不存在的值"
- Output: "404 响应，错误码 CERT_NOT_FOUND，消息为固定安全文案"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效登录会话（未认证）"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
    state_requirements:
      - description: "请求方未认证"
        prerequisite_entity: "DiscoveryImportSession"
- Input: "不带有效凭证 GET /api/v1/certs/discovery/import/:sessionId"
- Output: "401 语义的认证失败响应，不返回任何会话数据"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 会话先持久化再异步执行，重开后进度可续看不丢结果
- 全部发现端点对非 OpsEngineer 角色一律 403

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "DiscoveryImportSession"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "按分支为 running / completed / partial_failed"
    - entity_type: "CloudAccount"
      min_count: 1
  state_requirements:
    - description: "not-found 分支无对应会话；timeout 分支整体限时到期"
      prerequisite_entity: "DiscoveryImportSession"
```

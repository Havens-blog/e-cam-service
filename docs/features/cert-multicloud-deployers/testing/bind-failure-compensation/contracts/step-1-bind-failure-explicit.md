---
journey: "bind-failure-compensation"
step: 1
step-action: "绑定段执行失败并显式报错"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/bind-failure-compensation/journey.md
skip_eval: true
---

# Contract: bind-failure-compensation / Step 1: 绑定段执行失败并显式报错

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "bind-failure-explicit"
- Preconditions: "三云条目处于绑定段执行（上传段已完成、映射 active 已落库）；绑定被云侧拒绝或目标资源状态不允许（非限流类错误）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running（绑定段）"
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "云侧对该绑定请求返回拒绝/资源状态不允许（注入式失败，非限流）"
        prerequisite_entity: "CloudAccount"
- Input: "系统对三云某条目执行绑定段，运维人员查看执行结果"
- Output: "变更条目进入失败态，失败因 EXEC_FAILED + 静态文案（适配层错误为静态文案 + 产品上下文，不含私钥/凭证片段，云端细节不进 API 响应）；该条目不进入验证窗口、不标记成功"
- State: "ChangeItem failed(EXEC_FAILED)；补偿链触发：映射 active→orphan 并对已上传证书执行 CleanupOrphan 补偿（详见 Step 2/3）"
- Side-effect: "补偿链 CleanupOrphan 云调用（best-effort）"

## Outcome "bind-rate-limited"
<!-- source: inferred -->
<!-- reasoning: Fact Table MC_RATE_LIMITED_ITEM（execute_service.go:682-691：限流哨兵 → MarkRateLimited + 有界退避）+ 有界重试核心（deployer_common.go:31-63）：限流类绑定失败与非限流失败走不同状态与重试路径 -->
- Preconditions: "绑定段云 API 返回限流（ErrCloudRateLimited 哨兵）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running（绑定段）"
    state_requirements:
      - description: "云侧触发限流（注入式限流响应）"
        prerequisite_entity: "CloudAccount"
- Input: "系统执行绑定段并遇云侧限流"
- Output: "条目标记 rate_limited 并按有界退避自动重试（默认 5 次、总等待封顶）；退避耗尽后条目转 failed 记静态失败因，不无限重试"
- State: "条目 rate_limited →（重试成功）success 或（耗尽）failed"
- Side-effect: "云绑定 API 限流重试调用（有界）"

## Outcome "interrupted-before-bind"
- Preconditions: "UploadCert 已返回云证书 ID 且映射 active 已落库，进程在 BindResource 执行前崩溃/重启（心跳停更超时阈值）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running 且心跳时间已超时"
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active（上传后绑定前）"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "运维人员在恢复后查看该条目状态并重跑"
- Output: "心跳超时恢复将条目标记 failed(EXEC_TIMEOUT)（默认 30 分钟阈值、恢复任务周期巡检并告警）；悬空的上传证书可经补偿链路收敛（orphan 入清理队列），重跑时重新走两段式上传新副本，不复用悬空 ID 直接绑定"
- State: "条目 failed(EXEC_TIMEOUT)；重跑新执行按映射唯一键覆盖旧记录"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效会话"
  fixture_spec:
    entities:
      - entity_type: "ChangeOrder"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "executing"
    state_requirements:
      - description: "无有效会话"
        prerequisite_entity: "ChangeOrder"
- Input: "POST /api/v1/certs/changes/:id/execute 无有效会话调用"
- Output: "HTTP 401 全局认证失败文案（非 cert 模块 Envelope）"
- State: "变更单状态不变，不派发执行"
- Side-effect: "none"

## Journey Invariants
- 绑定失败必经 CleanupOrphan 补偿，且补偿幂等：任何重试/并发路径不重复删除云资源
- 失败状态、orphan 迁移、清理队列与 aliyun/tencent 走同一状态机，三云不新增补偿机制
- 云端错误细节仅入日志不进 API 响应；失败原因对用户呈现为静态文案

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeItem"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "explicit/rate-limited: running；interrupted: running 且心跳超时"
    - entity_type: "CloudCertMapping"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "active"
    - entity_type: "CloudAccount"
      min_count: 1
```

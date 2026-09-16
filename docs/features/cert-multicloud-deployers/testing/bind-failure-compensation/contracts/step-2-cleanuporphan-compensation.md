---
journey: "bind-failure-compensation"
step: 2
step-action: "触发 CleanupOrphan 补偿"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/bind-failure-compensation/journey.md
skip_eval: true
---

# Contract: bind-failure-compensation / Step 2: 触发 CleanupOrphan 补偿

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "compensation-success"
- Preconditions: "绑定失败已触发补偿链：映射可按云证书 ID 定位（active→orphan 迁移先于删除执行）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "orphan（补偿链迁移后）"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "系统按两段式编排对失败条目执行 CleanupOrphan 补偿，运维人员确认补偿结果"
- Output: "已上传到云证书库的新证书被删除（华为 SCM 删除证书 / AWS ACM 删除证书且仅限 imported 类型 / Azure KV 按证书名删除全部版本且不执行彻底清除）；补偿结果幂等可重入"
- State: "映射 status=orphan；云侧证书删除完成；补偿链标记完成"
- Side-effect: "云删除 API 调用（经限流与有界退避）；凭据用后归零"

## Outcome "double-invocation-idempotent"
- Preconditions: "同一条目的补偿被触发两次（重试与队列消费竞争）；第二次调用时云侧证书已不存在（首次已删）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "orphan"
    state_requirements:
      - description: "云侧目标证书已不存在（首次补偿已删除，或人工已删）"
        prerequisite_entity: "CloudAccount"
- Input: "系统第二次执行 CleanupOrphan"
- Output: "二次调用不报错：各云对已不存在按幂等成功语义返回（含 Azure 软删除态重复删除返回未找到）；不重复删除云侧资源，结果与单次调用一致（幂等对齐既有 CleanupOrphan 语义）"
- State: "映射保持 orphan（或已由清理消费删除）；无重复删除、无新增副作用"
- Side-effect: "云删除 API 幂等重放调用"

## Outcome "mapping-missing-compensation-incomplete"
<!-- 事实对齐：映射先于绑定写入（cloud_api_channel.go:186-196），"映射未落库"在本实现不成立；真实边界是补偿时按云证书 ID 查找映射未命中（cloud_api_channel.go:221-239 返回补偿未完成并保留 OrphanCandidate）。 -->
- Preconditions: "补偿执行时按云证书 ID 查找映射未命中（记录缺失或已被删除）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "无该云证书 ID 对应的映射记录（注入式缺失），云侧证书存在待删"
        prerequisite_entity: "CloudCertMapping"
- Input: "系统执行补偿链路"
- Output: "仍对云侧证书尽力 CleanupOrphan 删除；补偿结果标记未完成（部署结果保留孤儿候选标记，错误附 orphan compensation incomplete 语义），不产生脏映射记录"
- State: "无映射状态迁移（无记录可迁）；云侧删除尽力执行，孤儿候选标记保留待后续收敛"
- Side-effect: "云删除 API 调用（尽力）"

## Journey Invariants
- 绑定失败必经 CleanupOrphan 补偿，且补偿幂等：任何重试/并发路径不重复删除云资源
- 失败状态、orphan 迁移、清理队列与 aliyun/tencent 走同一状态机，三云不新增补偿机制
- 云端错误细节仅入日志不进 API 响应；失败原因对用户呈现为静态文案

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "CloudCertMapping"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "orphan（mapping-missing 分支无记录）"
    - entity_type: "CloudAccount"
      min_count: 1
  state_requirements:
    - description: "double-invocation 分支云侧证书已不存在；mapping-missing 分支云侧证书存在"
      prerequisite_entity: "CloudAccount"
```

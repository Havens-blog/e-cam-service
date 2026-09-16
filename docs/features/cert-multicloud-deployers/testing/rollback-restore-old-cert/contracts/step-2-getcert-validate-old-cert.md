---
journey: "rollback-restore-old-cert"
step: 2
step-action: "GetCert 校验旧云证书仍有效"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/rollback-restore-old-cert/journey.md
skip_eval: true
---

# Contract: rollback-restore-old-cert / Step 2: GetCert 校验旧云证书仍有效

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "precheck-pass"
- Preconditions: "回滚编排进入预检阶段；条目持久化字段中的旧云证书 ID 非空"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "success（持有旧云证书 ID）"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "云侧旧证书存在且未过期，指纹与变更单旧证书指纹一致"
        prerequisite_entity: "Certificate"
- Input: "系统调用 GetCert（InspectCloudCert）按旧云证书 ID 回读校验"
- Output: "旧证书存在且有效（含证书体/公钥可取回）：存在标记为真、有效期晚于当前、指纹与变更单旧证书指纹一致 → 预检通过后才继续回滚"
- State: "无状态迁移（只读校验）；凭据用后归零"
- Side-effect: "云 GetCert API 调用（华为经证书导出复核 SHA-256，复核失败按无法复核 fail-safe 处理）"

## Outcome "rollback-target-invalid"
- Preconditions: "云侧旧证书已不存在（人工删除）或已过期，或回读指纹与变更单旧证书指纹不符；任一判定即整单阻断"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "success（持有旧云证书 ID）"
    state_requirements:
      - description: "云侧旧证书缺失/过期/指纹不符（注入式回读结果）"
        prerequisite_entity: "CloudAccount"
- Input: "系统执行回滚预检"
- Output: "409 ROLLBACK_TARGET_INVALID 显式失败不猜测：回滚中止并给出明确原因，不盲绑到无效证书、不产生半回滚状态；审计记录回滚目标无效"
- State: "全部条目与变更单状态不变（无状态变更）"
- Side-effect: "none"

## Outcome "cross-cloud-form-mismatch-rejected"
<!-- source: inferred -->
<!-- reasoning: journey 原意为"每云解析各自 ID 形态、无跨云混淆"；以互斥的反向路径编码：ID 形态/凭据云域与目标云不匹配时显式拒绝（Fact Table MC_CREDS_CLOUD_PIN：deployer_common.go:85-98 凭据云域不匹配拒绝；各云 GetCert 按云实现解析） -->
- Preconditions: "回滚校验收到的旧 ID 形态与目标云不匹配，或凭据云域与部署器云不一致（如 ACM ARN 交由华为部署器解析）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
    state_requirements:
      - description: "ID 形态或凭据云域与目标部署器不匹配（注入式错配）"
        prerequisite_entity: "CloudAccount"
- Input: "系统对错配的云域/形态执行回滚校验"
- Output: "显式失败不猜测：按云解析失败/云域不匹配拒绝，不误判有效、不跨云混用 ID 空间"
- State: "无状态迁移"
- Side-effect: "none"

## Journey Invariants
- 回滚前必须经 GetCert 校验旧云证书有效，禁止在未知状态下盲绑
- 三云旧 ID 形态（SCM ID / ACM ARN / KV 引用）解析与校验按云实现，映射记录全程可追溯
- 预检基础设施错误 fail-safe 阻断，不误判有效

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeItem"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "success（持有旧云证书 ID）"
    - entity_type: "CloudAccount"
      min_count: 1
  state_requirements:
    - description: "precheck-pass: 云侧旧证书有效；rollback-target-invalid: 云侧缺失/过期/指纹不符；cross-cloud: 注入错配"
      prerequisite_entity: "CloudAccount"
```

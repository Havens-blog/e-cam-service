---
journey: "three-cloud-product-matrix"
step: 5
step-action: "Azure 两产品执行两段式（KV 引用绑定）"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/three-cloud-product-matrix/journey.md
skip_eval: true
---

# Contract: three-cloud-product-matrix / Step 5: Azure 两产品执行两段式（KV 引用绑定）

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "azure-two-products-success"
- Preconditions: "Azure cdn（Front Door）/alb（Application Gateway）组合引用就绪；Key Vault 实例已预置（装配注入或环境变量提供 vault 名称/URI）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 2
        field_constraints:
          - field: "product"
            value: "Azure cdn 与 alb 各一，status=pending"
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "azure"
    state_requirements:
      - description: "Key Vault 可用（env AZURE_KEY_VAULT_NAME/URI 或 WithKeyVaultName/URI 注入）"
        prerequisite_entity: "CloudAccount"
- Input: "运维人员对 Azure cdn（Front Door）/alb（Application Gateway）两组合执行上传与绑定"
- Output: "证书经 KV 证书导入 REST（PEM 路径）上传产生版本化 KV secret ID 引用；Front Door 按 KV 证书来源的自定义 HTTPS 启用补丁绑定、App Gateway 经 SSL 证书子资源写 KV secret 引用绑定；均成功并落 KV 引用形态映射"
- State: "两组合条目 success；两条 KV secret ID 形态 active 映射"
- Side-effect: "KV/ARM API 调用（KV 20 QPS 请求计数限流）；私钥材料随请求体用后归零"

## Outcome "appgw-inline-form-rejected"
- Preconditions: "App Gateway 组合的 SSL 证书资源为 inline data 形态（或绑定被按直传 ID 语义执行）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "product"
            value: "Azure alb（App Gateway）"
    state_requirements:
      - description: "目标 SSL 证书资源为 inline data 形态（注入式资源形态）"
        prerequisite_entity: "CloudAccount"
- Input: "系统对 App Gateway 组合执行绑定段"
- Output: "inline data 形态被显式拒绝（要求 KV-backed 证书资源，不猜测转换）；KV-backed 资源按 KV secret 引用语义绑定成功且引用指向本次上传的证书；引用已等于目标时幂等跳过"
- State: "KV-backed 场景：SSL 证书资源指向本次上传的 KV secret 引用；inline 场景：条目失败无脏写入"
- Side-effect: "ARM 子资源写入调用（KV-backed 场景）"

## Outcome "kv-missing-explicit-failure"
- Preconditions: "Azure 账号下不存在可用 Key Vault 实例，且装配与环境变量均未提供 vault 名称或 URI"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "product"
            value: "Azure cdn 或 alb"
    state_requirements:
      - description: "无 Key Vault 实例且 vault 配置为空（env 与装配均未注入）"
        prerequisite_entity: "CloudAccount"
- Input: "系统对 Azure 组合执行上传段"
- Output: "显式失败并提示前置资源缺失（需要 vault 目标：选项注入或环境变量），不猜测默认实例、不静默降级；其余云组合不受影响"
- State: "Azure 条目失败（EXEC_FAILED 静态文案）；无映射写入"
- Side-effect: "none"

## Journey Invariants
- 云证书 ID 形态按云固定：Azure KV secret ID 引用；形态校验失败显式报错不猜测
- Azure 绑定一律走 KV 证书引用语义（Front Door 源标记 + App Gateway secret 引用），不直传证书 ID
- 单组合失败隔离：任一组合的失败/跳过不影响其余组合的执行与终态

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeItem"
      min_count: 2
      field_constraints:
        - field: "product"
          value: "Azure cdn 与 alb"
    - entity_type: "CloudAccount"
      min_count: 1
      field_constraints:
        - field: "provider"
          value: "azure"
```

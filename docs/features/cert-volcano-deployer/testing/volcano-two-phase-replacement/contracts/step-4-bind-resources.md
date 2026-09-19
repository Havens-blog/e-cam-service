---
journey: "volcano-two-phase-replacement"
step: 4
step-action: "执行批次——第二段绑定资源"
generated: "2026-09-19"
sources:
  - docs/features/cert-volcano-deployer/testing/volcano-two-phase-replacement/journey.md
skip_eval: true
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: volcano-two-phase-replacement / Step 4: 执行批次——第二段绑定资源

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "success"
- Preconditions: "第一段上传已成功且映射落 active；绑定目标产品为 CDN/WAF/ALB/NLB 之一；云证书 ID 前缀与目标产品同库（alb/nlb 互跨允许）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
      - entity_type: "CertReference"
        min_count: 1
        field_constraints:
          - field: "cloud"
            value: "volcano"
- Input: "编排续走第二段 BindResource（CDN=BatchDeployCert 加速域名；WAF=防护域名证书替换；ALB/NLB=监听证书更新）"
- Output: "各产品资源证书引用切换为新证书，item 状态 success；绑定引用的是上传产物 ID（该产品库可绑定证书）"
- State: "线上资源证书引用指向新云证书 ID；item 终态 success 落库"
- Side-effect: "云侧写操作：对应产品资源证书置位绑定（各绑定 API 均为置位语义，重绑收敛同结果）"

## Outcome "bind-product-failure-isolated"
- Preconditions: "同 success 基线，但某一产品的绑定调用失败（如 CDN 域名粒度与 ListReferences 不对齐或云侧拒绝）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
    state_requirements:
      - description: "同批含多产品项且仅一项绑定调用失败"
        prerequisite_entity: "CloudCertMapping"
- Input: "执行绑定"
- Output: "该 item 结构化失败（静态 reason，云侧细节仅日志），其他产品项不受影响；失败触发补偿语义（见 journey volcano-bind-failure-compensation）"
- State: "失败 item 状态落库；其他产品 item 不受牵连"
- Side-effect: "绑定失败的项进入补偿分支（映射 active→orphan + 尽力清理孤儿）"

## Journey Invariants
- 绑定引用上传产物 ID（该产品库可绑定证书），csv 统一库实例不可直接绑定产品资源
- 各绑定 API 为置位语义：同一 (resource, cloudCertID) 重绑收敛同结果
- 云侧错误细节不进响应仅日志；私钥明文仅内存传递用后 Zeroize

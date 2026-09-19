---
journey: "volcano-rollback-verify-window"
step: 2
step-action: "回滚发起与 GetCert 目标校验"
generated: "2026-09-19"
sources:
  - docs/features/cert-volcano-deployer/testing/volcano-rollback-verify-window/journey.md
skip_eval: true
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: volcano-rollback-verify-window / Step 2: 回滚发起与 GetCert 目标校验

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "valid-target"
- Preconditions: "回滚已发起；旧证书云 ID 在映射中可查（替换前映射）；旧证书在火山证书库且可用"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "certFingerprint"
            value: "旧证书指纹（替换前映射记录）"
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
- Input: "运维发起回滚，编排经 GetCert 校验旧云证书 ID 有效（三判定：在库、可用、与台账指纹一致）"
- Output: "旧 ID 判定有效；WAF/ALB/NLB 无指纹通道场景经映射回退解析旧 ID 与要素，判定不因通道缺失而失败"
- State: "回滚流程进入逐产品恢复阶段"
- Side-effect: "none（GetCert 只读）"

## Outcome "invalid-target-rejected"
- Preconditions: "回滚已发起，但旧云证书已被云侧删除（GetCert 判定不在库/不可用）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
    state_requirements:
      - description: "火山证书库中旧证书已不存在或不可用（Exists=false）"
        prerequisite_entity: "CloudAccount"
- Input: "发起回滚"
- Output: "回滚拒绝且不误绑（三判定拦截），呈报回滚目标无效的静态 reason，线上保持新绑定现状"
- State: "线上资源证书引用不变（保持新证书）；回滚未执行"
- Side-effect: "none"

## Outcome "mapping-fallback-no-record"
- Preconditions: "WAF/ALB/NLB 资源无指纹通道，且映射中无旧 ID 记录（反查无果）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 0
      - entity_type: "CertReference"
        min_count: 1
        field_constraints:
          - field: "product"
            value: "waf 或 alb 或 nlb（无指纹通道产品）"
    state_requirements:
      - description: "映射反查与 GetCert 均无法产出旧证书指纹复核依据"
        prerequisite_entity: "CertReference"
- Input: "发起回滚"
- Output: "映射回退无果时按既有占位/失败语义呈报（确定性占位指纹 certscan-unresolved 口径），不伪造有效性判定"
- State: "回滚判定 fail-safe 阻断转人工，线上状态不变"
- Side-effect: "none"

## Outcome "non-normalized-old-id"
<!-- source: inferred -->
<!-- reasoning: Fact Table 显示 GetCert 对非归一云证书 ID fail-fast（volcano_deployer.go:1219-1221）——旧 ID 脏数据流入回滚判定是现实边界，必须阻断而非猜测 -->
- Preconditions: "回滚发起，但映射中的旧云证书 ID 为非 {product}:{id} 归一形态"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "cloudCertID"
            value: "非归一形态（缺前缀/未知前缀/空裸 ID）"
- Input: "发起回滚"
- Output: "GetCert fail-fast 结构化失败（要求归一形态），不猜测路由，回滚阻断"
- State: "线上状态不变；错误可诊断"
- Side-effect: "none"

## Journey Invariants
- 回滚前必经 GetCert 目标有效性判定（三判定），无效目标不误绑
- WAF/ALB/NLB 无指纹通道不阻塞回滚判定——GetCert 映射回退兜底
- 判定 fail-safe：无法复核指纹即阻断转人工，不伪造有效性

---
journey: "volcano-rollback-verify-window"
step: 3
step-action: "逐产品恢复旧绑定"
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

# Contract: volcano-rollback-verify-window / Step 3: 逐产品恢复旧绑定

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "restore-success"
- Preconditions: "回滚目标经 GetCert 判定有效；四产品（CDN/WAF/ALB/NLB）均存在需恢复的资源"
  fixture_spec:
    entities:
      - entity_type: "CertReference"
        min_count: 4
        field_constraints:
          - field: "product"
            value: "覆盖 cdn/waf/alb/nlb 四产品"
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "cloudCertID"
            value: "旧证书归一 ID"
- Input: "编排对四产品分别执行 BindResource 恢复旧 ID（CDN BatchDeployCert / WAF 域名替换 / ALB-NLB 监听更新）"
- Output: "各产品资源证书引用恢复为旧证书，逐产品状态如实落账（每产品覆盖，不因某产品失败跳过呈报）"
- State: "线上四产品资源证书引用均指向旧云证书 ID"
- Side-effect: "云侧写操作：各产品绑定 API 置位恢复"

## Outcome "partial-product-failure"
- Preconditions: "四产品回滚中某产品 BindResource 失败（如 CDN 域名已下线）"
  fixture_spec:
    entities:
      - entity_type: "CertReference"
        min_count: 2
        field_constraints:
          - field: "product"
            value: "至少两个产品（其一恢复失败）"
    state_requirements:
      - description: "某产品的绑定调用失败（资源已下线或云侧拒绝）"
        prerequisite_entity: "CertReference"
- Input: "执行回滚"
- Output: "失败产品如实呈报静态 reason，其他产品照常恢复；失败项可重试且幂等（重跑同结果）"
- State: "成功产品恢复旧绑定；失败产品保持新绑定现状且状态如实"
- Side-effect: "云侧写操作：仅成功产品发生绑定恢复"

## Outcome "rerun-idempotent"
- Preconditions: "回滚已成功完成（重复触发回滚）"
  fixture_spec:
    entities:
      - entity_type: "CertReference"
        min_count: 1
        field_constraints:
          - field: "referenced cloud cert id"
            value: "已恢复为旧证书 ID"
- Input: "再次触发回滚"
- Output: "幂等——不重复绑定、状态同结果，无重复映射行"
- State: "映射无重复行；线上绑定保持旧证书不变"
- Side-effect: "none（置位语义重绑收敛同结果）"

## Outcome "listener-not-found-cross-region"
<!-- source: inferred -->
<!-- reasoning: Fact Table 显示 ALB/NLB 监听绑定按账号地域逐地域定位、耗尽未命中即报 not found（volcano_deployer.go:775）——监听跨地域缺失/已删除是回滚恢复现实边界 -->
- Preconditions: "ALB/NLB 回滚目标监听在账号所有地域均无法定位（已删除或 ID 漂移）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
    state_requirements:
      - description: "监听定位在所有配置地域均未命中"
        prerequisite_entity: "CloudAccount"
- Input: "对该监听执行回滚恢复"
- Output: "结构化失败（监听在配置地域中未找到，附地域清单可诊断），其他产品照常恢复"
- State: "失败产品状态如实落库；线上无误绑"
- Side-effect: "none"

## Journey Invariants
- 回滚逐产品覆盖与呈报，失败可重试且幂等
- 各绑定 API 置位语义：恢复重绑收敛同结果
- 云侧错误细节仅日志（静态 reason 呈报）

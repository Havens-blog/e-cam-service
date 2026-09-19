---
journey: "volcano-two-phase-replacement"
step: 1
step-action: "引用扫描发现火山引用并生成变更清单"
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

# Contract: volcano-two-phase-replacement / Step 1: 引用扫描发现火山引用并生成变更清单

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "success"
- Preconditions: "火山账号已登记且扫描适配器已装配（第 6 云）；台账存在旧/新证书 fingerprint 配对（新证书含私钥）；火山 CDN/WAF/ALB/NLB 各有资源引用旧证书指纹"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
      - entity_type: "CertificateLedgerEntry"
        min_count: 2
        field_constraints:
          - field: "new cert has private key"
            value: "true (非 fingerprint_only)"
      - entity_type: "CertReference"
        min_count: 1
        field_constraints:
          - field: "cloud"
            value: "volcano"
    state_requirements:
      - description: "火山扫描适配器已注册进扫描适配器列表（第 6 云装配完成）"
        prerequisite_entity: "CloudAccount"
- Input: "触发引用扫描并对扫描结果执行清单生成评估"
- Output: "火山四产品引用进入变更清单，逐项 AutoChangeable=true（cloud_api 通道），Target.Cloud=volcano 且产品归属正确；不出现 ERR_DISCOVERY_ONLY 或 skipped"
- State: "扫描快照与 CertReference 落库；变更清单处于待确认状态"
- Side-effect: "none（引用扫描全程只读，无云侧写操作）"

## Outcome "fingerprint-only-blocked"
- Preconditions: "同 success 基线，但台账新证书为 fingerprint_only（无私钥材料）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
      - entity_type: "CertificateLedgerEntry"
        min_count: 1
        field_constraints:
          - field: "private key"
            value: "缺失（fingerprint_only）"
      - entity_type: "CertReference"
        min_count: 1
        field_constraints:
          - field: "cloud"
            value: "volcano"
- Input: "清单生成评估该火山引用"
- Output: "该引用被拦截为不可执行（与五云一致的既有通用语义），不进入 AutoChangeable 面，原因呈报静态可诊断"
- State: "变更清单中该项不标记为可自动变更；台账与引用状态不变"
- Side-effect: "none"

## Outcome "mapping-fallback-fingerprint"
- Preconditions: "同 success 基线，但目标为火山 WAF/ALB/NLB 资源（无独立指纹通道）且映射反查与 GetCert 要素均无法产出对齐指纹"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
      - entity_type: "CertReference"
        min_count: 1
        field_constraints:
          - field: "product"
            value: "waf 或 alb 或 nlb（无指纹通道产品）"
- Input: "引用扫描解析该资源指纹"
- Output: "经映射反查回退 → GetCert 要素 → 确定性占位指纹（certscan-unresolved 口径）逐级回退，CertReference 指纹字段不缺失"
- State: "CertReference 落库且携带可对账指纹（真实或确定性占位）"
- Side-effect: "none"

## Journey Invariants
- 火山与五云同一状态机、同一两段式编排；差异仅在证书库形态与绑定 API
- 云证书 ID 形态恒为 {product}:{id} 前缀归一，四产品库 ID 空间互斥不跨库误引
- 引用扫描只读；写操作仅存在于执行面（deployer）
- 云侧错误细节不进响应仅日志；私钥明文仅内存传递用后 Zeroize

---
journey: "volcano-dispatch-visibility"
step: 1
step-action: "火山引用进入扫描"
generated: "2026-09-19"
sources:
  - docs/features/cert-volcano-deployer/testing/volcano-dispatch-visibility/journey.md
skip_eval: true
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: volcano-dispatch-visibility / Step 1: 火山引用进入扫描

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "success"
- Preconditions: "module.go 装配完成（volcano×4 产品部署器 + 扫描适配器加入适配器列表）；火山账号已登记；线上存在火山四产品引用旧证书的资源"
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
          - field: "cloud"
            value: "volcano"
    state_requirements:
      - description: "火山扫描适配器在扫描适配器列表中启用（第 6 云装配）"
        prerequisite_entity: "CloudAccount"
- Input: "触发引用扫描，扫描适配器枚举火山四产品资源"
- Output: "火山资源进入 CertReference 面（指纹解析对齐既有口径：映射反查 → GetCert 要素 → 确定性占位指纹），四产品逐项可见"
- State: "扫描快照与 CertReference 落库"
- Side-effect: "none（扫描全程只读无云侧写操作）"

## Outcome "scan-api-failure-isolated"
- Preconditions: "同 success 基线，但火山扫描 API 调用失败"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
    state_requirements:
      - description: "火山扫描 API 返回错误；同轮次含其他云扫描"
        prerequisite_entity: "CloudAccount"
- Input: "触发引用扫描"
- Output: "失败以静态 reason 隔离呈现（云级隔离，对齐五云失败隔离先例），其他云扫描结果不受影响"
- State: "其他云的 CertReference 正常落库；火山项失败原因可诊断"
- Side-effect: "none"

## Outcome "unknown-product-placeholder-fingerprint"
- Preconditions: "扫描遇到四产品之外或形态未知的火山资源"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
    state_requirements:
      - description: "存在四产品之外或形态未知的火山资源条目"
        prerequisite_entity: "CloudAccount"
- Input: "解析该资源指纹"
- Output: "走确定性占位指纹（certscan-unresolved 语义一致），不 panic 不误判指纹"
- State: "该资源以占位指纹进入引用面或被安全跳过，服务不中断"
- Side-effect: "none"

## Journey Invariants
- 引用扫描只读；写操作仅存在于执行面
- 云侧扫描失败静态 reason 隔离，不进响应细节
- 未知产品形态不 panic，走确定性占位指纹口径

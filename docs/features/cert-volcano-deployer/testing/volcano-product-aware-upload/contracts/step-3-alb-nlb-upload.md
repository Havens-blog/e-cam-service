---
journey: "volcano-product-aware-upload"
step: 3
step-action: "ALB/NLB 产品定向上传"
generated: "2026-09-19"
sources:
  - docs/features/cert-volcano-deployer/testing/volcano-product-aware-upload/journey.md
skip_eval: true
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: volcano-product-aware-upload / Step 3: ALB/NLB 产品定向上传

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "success"
- Preconditions: "火山部署器已装配；证书束在台账就绪；目标产品为 ALB 或 NLB"
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
            value: "存在"
- Input: "对火山 ALB / NLB 监听引用分别执行第一段上传（UploadCertForProduct product=alb|nlb，经 ALB 监听证书库上传 API，C7 名经 CertificateName 承载）"
- Output: "监听证书上传至对应产品库，云证书 ID 归一形态 alb:{id} / nlb:{id}"
- State: "ALB 监听证书库新增证书（alb/nlb 共用同一库）；映射可落 active"
- Side-effect: "云侧写操作：监听证书库新增证书；私钥明文仅内存传递用后 Zeroize"

## Outcome "id-space-mutex"
- Preconditions: "同一证书束已分别上传至四个产品库（csv/cdn/waf/alb-nlb 各有产物）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 4
        field_constraints:
          - field: "cloudCertID"
            value: "四条归一 ID 前缀分别为 csv/cdn/waf/alb 或 nlb"
- Input: "核对四条云证书 ID 的归一形态"
- Output: "{product}:{id} 前缀互斥、同库内 id 不冲突；跨产品不误引（ID 归一断言口径，对齐三云 ID 空间互斥测试先例）"
- State: "四条映射行各自绑定正确前缀的云证书 ID，互不混淆"
- Side-effect: "none（只读核对）"

## Outcome "empty-cloud-id-structural-failure"
<!-- source: inferred -->
<!-- reasoning: Fact Table 显示四产品上传分支均对云侧返回空 ID 显式报错（volcano_deployer.go:520-522/535-537/550-552/565-567）——SDK 成功但缺 ID 是现实边界，不能产出伪归一 ID -->
- Preconditions: "同 success 基线，但云侧上传响应缺失证书 ID 字段（空 ID）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
    state_requirements:
      - description: "云侧上传调用成功但响应中证书 ID 为空"
        prerequisite_entity: "CloudAccount"
- Input: "执行上传"
- Output: "结构化失败（空证书 ID 错误），不产出伪归一云证书 ID"
- State: "无映射落库；无悬空绑定"
- Side-effect: "none"

## Journey Invariants
- 云证书 ID 恒为 {product}:{id} 前缀归一，产品库 ID 空间互斥
- 上传失败不产生悬空绑定（两段式保证：先上传成功落映射 active 才进入 BindResource）
- 私钥明文仅内存传递、用后 Zeroize

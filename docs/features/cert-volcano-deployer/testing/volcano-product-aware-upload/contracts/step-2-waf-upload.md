---
journey: "volcano-product-aware-upload"
step: 2
step-action: "WAF 产品定向上传"
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

# Contract: volcano-product-aware-upload / Step 2: WAF 产品定向上传

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "success"
- Preconditions: "火山部署器已装配；证书束在台账就绪；目标产品为 WAF"
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
- Input: "对火山 WAF 域名引用执行第一段上传（UploadCertForProduct product=waf，经 WAF 服务证书上传 API，C7 名承载 Name/Description）"
- Output: "上传为 WAF 服务证书，云证书 ID 归一形态 waf:{id}"
- State: "WAF 服务证书库新增证书；映射可落 active"
- Side-effect: "云侧写操作：WAF 服务证书库新增证书；私钥明文仅内存传递用后 Zeroize"

## Outcome "key-missing-intercepted"
- Preconditions: "台账证书为 fingerprint_only（无私钥材料）"
  fixture_spec:
    entities:
      - entity_type: "CertificateLedgerEntry"
        min_count: 1
        field_constraints:
          - field: "private key"
            value: "缺失（fingerprint_only）"
- Input: "执行上传路径"
- Output: "上传前拦截（清单生成评估语义 / 材料加载 ErrCertMaterialUnavailable），不发起云侧上传"
- State: "不产生孤儿库条目；台账状态不变"
- Side-effect: "none"

## Outcome "rate-limited-bounded-retry"
<!-- source: inferred -->
<!-- reasoning: Fact Table 显示火山部署器统一走 boundedRetry 有界重试（volcano_deployer.go:399-401），限流错误按固定序列退避且有次数与总时长双闸——限流是云 API 现实边界 -->
- Preconditions: "同 success 基线，但火山证书库 API 持续返回限流错误直至重试额度耗尽"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
    state_requirements:
      - description: "火山云 API 对上传请求持续返回限流错误"
        prerequisite_entity: "CloudAccount"
- Input: "执行上传"
- Output: "按固定序列退避重试（次数与总时长双闸，禁止无限重试），耗尽后返回包装的末次错误（哨兵语义保留）"
- State: "无映射 active 落库（上传未成功）；无孤儿绑定"
- Side-effect: "none（重试期间未成功则无云侧持久产物）"

## Journey Invariants
- 云证书 ID 恒为 {product}:{id} 前缀归一，产品库 ID 空间互斥
- 限流退避重试有上限次数与总时长双闸，禁止无限重试
- 私钥明文仅内存传递、用后 Zeroize；云侧错误细节仅日志

---
journey: "volcano-product-aware-upload"
step: 1
step-action: "CDN 产品定向上传"
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

# Contract: volcano-product-aware-upload / Step 1: CDN 产品定向上传

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "success"
- Preconditions: "火山部署器已装配；证书束（证书+链+私钥）在台账就绪；目标产品为 CDN"
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
- Input: "对火山 CDN 引用执行两段式第一段上传（UploadCertForProduct product=cdn，经 CDN 证书库 AddCertificate，C7 名经 Desc 承载）"
- Output: "上传至 CDN 证书库，返回云证书 ID 归一形态 cdn:{id}"
- State: "CDN 证书库新增证书条目；映射可落 active"
- Side-effect: "云侧写操作：CDN 证书库新增证书；私钥明文仅内存传递用后 Zeroize"

## Outcome "upload-name-conflict-idempotent"
- Preconditions: "同一证书束已对同一产品（CDN）上传过（重复触发上传）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
      - entity_type: "CertificateLedgerEntry"
        min_count: 1
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
- Input: "再次执行第一段上传"
- Output: "上传名逐次唯一生成（ecam-{指纹前8}-{unix秒}-{随机后缀}），云证书库不产生语义重复条目（按云侧语义去重或新条目可被映射收敛），映射不重复落行"
- State: "映射仓储中该 (指纹, 云, 账号, 云证书 ID) 不出现重复行"
- Side-effect: "云侧写操作：仅新增可收敛的证书条目，无重复映射"

## Outcome "chain-missing-root-fallback"
- Preconditions: "同 success 基线，但证书束链缺根（火山侧校验会拒）"
  fixture_spec:
    entities:
      - entity_type: "CertificateLedgerEntry"
        min_count: 1
        field_constraints:
          - field: "cert chain"
            value: "缺根"
    state_requirements:
      - description: "火山证书库对缺根链返回校验拒绝"
        prerequisite_entity: "CertificateLedgerEntry"
- Input: "执行上传"
- Output: "checkChain 系统信任库回退先消解可补链场景；仍不可信则结构化失败并呈报静态 reason（云侧细节仅日志）"
- State: "上传失败不落映射 active，不产生悬空绑定"
- Side-effect: "none（上传未成功）"

## Outcome "unsupported-product-sentinel"
<!-- source: inferred -->
<!-- reasoning: Fact Table 显示 UploadCertForProduct 仅接受四绑定目标产品（volcanoBindCertProducts，volcano_deployer.go:459-464），csv 等非绑定产品传入即返回 ErrVolcanoProductNotSupported 哨兵——产品定向入参越界是现实边界 -->
- Preconditions: "部署器已装配，但产品定向入参为非绑定目标产品（如 csv/clb）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
- Input: "以产品定向端口传入非绑定目标产品（如 csv）执行上传"
- Output: "返回产品不支持的哨兵错误（ErrVolcanoProductNotSupported），不发起云侧调用"
- State: "无状态变化，无云侧写操作"
- Side-effect: "none"

## Journey Invariants
- 云证书 ID 恒为 {product}:{id} 前缀归一，产品库 ID 空间互斥
- 每次上传尝试（含重试）生成全新上传名（C7：重试即新副本，孤儿清理兜底）
- 私钥明文仅内存传递、用后 Zeroize；云侧错误细节仅日志

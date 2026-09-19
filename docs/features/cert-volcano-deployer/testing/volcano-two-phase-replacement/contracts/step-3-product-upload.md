---
journey: "volcano-two-phase-replacement"
step: 3
step-action: "执行批次——产品定向第一段上传"
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

# Contract: volcano-two-phase-replacement / Step 3: 执行批次——产品定向第一段上传

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "success"
- Preconditions: "变更单已确认且批次已排程；目标火山引用的产品为 CDN/WAF/ALB/NLB 之一；台账证书束（证书+链+私钥）可加载"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "可执行（已确认）"
      - entity_type: "CertificateLedgerEntry"
        min_count: 1
        field_constraints:
          - field: "private key"
            value: "存在"
      - entity_type: "CloudCertMapping"
        min_count: 1
- Input: "触发批次执行，编排走两段式第一段产品定向上传（经 ProductAwareUploader 端口按 target.Product 路由对应产品证书库上传 API）"
- Output: "上传产物写入对应产品证书库，云证书 ID 为 {product}:{id} 前缀归一形态；映射先行落 active"
- State: "云证书库新增该产品库证书条目；CloudCertMapping 新增/更新为 active（两段式去重与崩溃恢复锚点）"
- Side-effect: "云侧写操作：对应产品证书库新增证书；私钥明文仅内存传递用后 Zeroize"

## Outcome "upload-failed-batch-interrupt"
- Preconditions: "同 success 基线，但火山证书库拒绝上传（如证书链缺根且系统信任库回退仍失败）"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "可执行（已确认）"
      - entity_type: "CertificateLedgerEntry"
        min_count: 1
        field_constraints:
          - field: "cert chain"
            value: "缺根且无法补链"
    state_requirements:
      - description: "火山证书库对该上传请求返回拒绝错误"
        prerequisite_entity: "ChangeList"
- Input: "执行批次"
- Output: "该 item 失败且错误呈报为静态 reason（云侧错误细节仅日志不进响应）；同批后续项按编排语义中断"
- State: "失败 item 状态落库为失败；不跨批污染其他批次；不产生可绑定的上传产物映射"
- Side-effect: "none（上传未成功则无云侧持久产物进入绑定路径）"

## Journey Invariants
- 云证书 ID 恒为 {product}:{id} 前缀归一，四产品库 ID 空间互斥
- 每次上传尝试（含重试）生成全新上传名（重试即新副本，孤儿清理兜底）
- 云侧错误细节不进响应仅日志；私钥明文仅内存传递用后 Zeroize

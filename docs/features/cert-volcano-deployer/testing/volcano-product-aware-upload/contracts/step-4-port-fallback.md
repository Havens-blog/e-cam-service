---
journey: "volcano-product-aware-upload"
step: 4
step-action: "可选端口缺失回退"
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

# Contract: volcano-product-aware-upload / Step 4: 可选端口缺失回退

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "fallback-transparent"
- Preconditions: "部署器实例未实现 ProductAwareUploader 可选端口（仅实现 CloudDeployer 五方法）；证书束在台账就绪"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "CertificateLedgerEntry"
        min_count: 1
        field_constraints:
          - field: "private key"
            value: "存在"
    state_requirements:
      - description: "通道中注册的部署器未升级 ProductAwareUploader 端口"
        prerequisite_entity: "CloudAccount"
- Input: "以未实现 ProductAwareUploader 端口的部署器实例执行上传路径"
- Output: "可选端口缺失不报错，通道按类型断言分发回退通用 UploadCert 上传语义（行为完全不变），调用方无感知"
- State: "上传产物映射形态可预期（非产品定向路径）"
- Side-effect: "云侧写操作：通用上传路径的证书库写入"

## Outcome "partial-port-implementation"
- Preconditions: "部署器仅对部分产品实现产品感知上传（部分实现形态）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "CertificateLedgerEntry"
        min_count: 1
    state_requirements:
      - description: "部署器产品感知上传能力部分实现（已实现与未实现产品并存）"
        prerequisite_entity: "CloudAccount"
- Input: "对已实现/未实现产品分别触发上传"
- Output: "已实现产品走定向路径，未实现产品走回退路径，两者产物映射形态一致（同 {product}:{id} 归一）"
- State: "两条路径产物均能落 active 映射且形态一致"
- Side-effect: "云侧写操作：按路径分别写入对应证书库"

## Outcome "retry-fresh-upload-name"
<!-- source: inferred -->
<!-- reasoning: Fact Table 显示上传主干每次尝试（含重试）生成全新名称、禁止复用可能已成功的名称（C7，volcano_deployer.go:441-443/484）——重试换名是防孤儿误判的关键约束 -->
- Preconditions: "上传首次尝试失败后触发重试（同证书束同产品）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "CertificateLedgerEntry"
        min_count: 1
    state_requirements:
      - description: "首次上传尝试已失败（云侧错误或网络瞬断）"
        prerequisite_entity: "CloudAccount"
- Input: "执行重试上传"
- Output: "重试使用全新生成的上传名（不复用可能已成功的名称），成功后产物可正常落映射"
- State: "若首次实际已成功（云侧视角），遗留副本成为孤儿候选，由孤儿清理兜底"
- Side-effect: "云侧写操作：重试可能产生额外证书副本（孤儿清理兜底）"

## Journey Invariants
- ProductAwareUploader 为可选端口：缺失不阻塞、不报错，回退通用上传语义（五云零影响）
- 上传名称逐次唯一，重复上传不产生重复映射
- 私钥明文仅内存传递、用后 Zeroize；云侧错误细节仅日志

---
journey: "volcano-dispatch-visibility"
step: 2
step-action: "清单生成判定可执行"
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

# Contract: volcano-dispatch-visibility / Step 2: 清单生成判定可执行

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "auto-changeable-true"
- Preconditions: "火山引用已进入 CertReference 面；火山部署器已注册（第 6 云装配完成）"
  fixture_spec:
    entities:
      - entity_type: "CertReference"
        min_count: 1
        field_constraints:
          - field: "cloud"
            value: "volcano"
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
- Input: "清单生成评估火山引用"
- Output: "火山引用 AutoChangeable=true（cloud_api 通道），不再 ERR_DISCOVERY_ONLY 或 skipped；清单项 Target.Cloud=volcano 且产品归属正确"
- State: "变更清单项带可执行判定与通道归属落库"
- Side-effect: "none"

## Outcome "five-cloud-regression-intact"
- Preconditions: "既有五云引用与新装配（火山）共存"
  fixture_spec:
    entities:
      - entity_type: "CertReference"
        min_count: 2
        field_constraints:
          - field: "cloud"
            value: "同时含既有五云引用与火山引用"
- Input: "清单生成并核对五云项"
- Output: "五云项判定与装配前一致（AutoChangeable/通道/部署器路由均无漂移），第 6 云新增零破坏"
- State: "五云清单项判定结果与回归基线一致"
- Side-effect: "none"

## Outcome "deployer-not-registered-skipped"
- Preconditions: "火山 deployer 未注册（装配缺失或被摘除），但火山引用存在"
  fixture_spec:
    entities:
      - entity_type: "CertReference"
        min_count: 1
        field_constraints:
          - field: "cloud"
            value: "volcano"
    state_requirements:
      - description: "执行通道中无火山部署器注册"
        prerequisite_entity: "CertReference"
- Input: "清单生成评估火山引用"
- Output: "回落既有 skipped/不可执行语义（不误报可执行），呈报原因可诊断，服务不 panic"
- State: "清单项标记不可自动变更并带原因；服务正常运行"
- Side-effect: "none"

## Journey Invariants
- 火山引用判定回归恒真：AutoChangeable=true，不再 ERR_DISCOVERY_ONLY/skipped（部署器已装配前提下）
- 第 6 云装配对既有五云行为零破坏（回归恒绿）

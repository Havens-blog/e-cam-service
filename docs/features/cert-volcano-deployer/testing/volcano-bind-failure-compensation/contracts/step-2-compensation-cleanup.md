---
journey: "volcano-bind-failure-compensation"
step: 2
step-action: "补偿清理孤儿"
generated: "2026-09-19"
sources:
  - docs/features/cert-volcano-deployer/testing/volcano-bind-failure-compensation/journey.md
skip_eval: true
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: volcano-bind-failure-compensation / Step 2: 补偿清理孤儿

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "compensation-success"
- Preconditions: "第二段绑定已失败；第一段上传的孤儿证书仍在火山证书库；映射为 active"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
- Input: "编排自动触发 CleanupOrphan（按归一前缀路由对应产品库删除 API）"
- Output: "第一段上传的孤儿证书经火山侧删除 API 删除，映射 active→orphan 入清理队列，不留悬空 active 引用"
- State: "云侧孤儿证书删除；映射状态转为 orphan（5.9 清理队列入口）"
- Side-effect: "云侧写操作：删除孤儿证书"

## Outcome "delete-api-failure-queued"
- Preconditions: "同 success 基线，但火山删除 API 调用失败（云侧瞬时错误）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
    state_requirements:
      - description: "火山侧删除调用返回瞬时错误"
        prerequisite_entity: "CloudCertMapping"
- Input: "触发补偿"
- Output: "补偿未竟不掩盖绑定失败原因；孤儿保留在清理队列（orphan 态不回退 active），后续轮次重试收敛"
- State: "映射转 orphan（或保留待重试），云侧孤儿证书仍在"
- Side-effect: "none（删除未成功）"

## Outcome "concurrent-retry-race"
- Preconditions: "item 失败补偿进行中，操作者并发触发重试执行"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active（补偿转换中）"
- Input: "并发操作（补偿与重试执行同时进行）"
- Output: "既有状态机语义收敛（复用五云并发口径），映射不出现 active/orphan 双态并存"
- State: "映射终态唯一（active 或 orphan 二者其一），无撕裂"
- Side-effect: "不确定（取决于并发次序，但终态收敛）"

## Outcome "mapping-reverse-lookup-failure"
<!-- source: inferred -->
<!-- reasoning: Fact Table 显示 compensateBindFailure 中映射反查失败时 complete=false、映射保留 active 供上层可见（cloud_api_channel.go:222-226）——反查失败是补偿链现实边界 -->
- Preconditions: "第二段绑定已失败，但按新云证书 ID 反查映射失败（映射记录缺失或仓储异常）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
    state_requirements:
      - description: "映射仓储中无该新云证书 ID 的 active 记录"
        prerequisite_entity: "CloudAccount"
- Input: "触发补偿"
- Output: "补偿标记未完成（映射未能转 orphan，保留 active 上层可见错误），云侧清理仍尽力执行"
- State: "映射保留 active（5.9 不消费），补偿完成度如实上报"
- Side-effect: "云侧写操作：尽力删除孤儿证书"

## Journey Invariants
- 绑定失败必触发补偿，不留悬空 active 映射
- 补偿失败不掩盖绑定失败原因呈报（静态 reason，云侧细节仅日志）
- 孤儿清理走同一映射状态机（active→orphan→清理队列），火山不引入新状态

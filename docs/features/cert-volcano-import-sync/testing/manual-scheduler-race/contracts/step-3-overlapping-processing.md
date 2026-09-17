---
journey: "manual-scheduler-race"
step: 3
step-action: "两轮处理重叠证书"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/manual-scheduler-race/journey.md
skip_eval: true
---

# Contract: manual-scheduler-race / Step 3: 两轮处理重叠证书

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "定时轮与手动轮（先后或竞态窗口内）差异集重叠同一指纹实例；两轮经 CAS 串行化各自持轮"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "CertLibraryInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "重叠指纹实例对两轮均未入账（各自增量集包含它）"
        prerequisite_entity: "Certificate"
- Input: "两轮各自处理同一指纹实例"
- Output: "两轮各自进入导入路径（判定层各自转导入条目），写入竞争被幂等语义接管（后到者撞指纹唯一键转补建映射）"
- State: "无部分写入残留；台账/映射收敛形态唯一"
- Side-effect: "none"

## Outcome "cross-cloud-same-fingerprint"
<!-- source: inferred -->
<!-- reasoning: journey Step 3b + Fact Table UK_FINGERPRINT（domain/repository.go:12-14）+ MAPPING_UNIQUE_KEY（cert_sync_service.go:30-32） -->
- Preconditions: "两云内容相同（同指纹）证书在同窗口被两轮分别处理"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "provider"
            value: "两个不同云"
      - entity_type: "CertLibraryInstance"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "fingerprint"
            value: "两实例指纹相同"
- Input: "双轮处理两实例"
- Output: "幂等收敛：台账恰 1 条，两云各自账号各建映射，无失败条目"
- State: "台账/映射收敛形态唯一"
- Side-effect: "none"

## Outcome "import-path-not-blocked-by-cas"
<!-- source: inferred -->
<!-- reasoning: Fact Table CERT_SYNC_CAS_GUARD（cert_sync_service.go:251-255，CAS 作用于 run 入口轮级而非 judgeInstance 条目级）+ journey Step 3 expected「两轮各自进入导入路径」 -->
- Preconditions: "两轮先后各持有 CAS（一轮执行中另一轮在其结束后启动）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "CertLibraryInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "后到轮对重叠指纹执行判定"
- Output: "导入路径不受 CAS 约束影响（CAS 守卫轮级而非条目级）：后到轮可正常判定与导入，重叠写入由幂等接管"
- State: "无死锁；无条目级阻塞"
- Side-effect: "none"

## Journey Invariants
- 台账指纹全局唯一：竞态双方不产生第二条同指纹台账
- CAS 守卫保证任一时刻至多一轮同步执行（调度面静默跳过、手动面 409）

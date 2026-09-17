---
journey: "first-sync-backfill"
step: 2
step-action: "枚举全部证书可达云 × active 账号"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/first-sync-backfill/journey.md
skip_eval: true
---

# Contract: first-sync-backfill / Step 2: 枚举全部证书可达云 × active 账号

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "证书可达云清单就绪（aliyun/tencent/huawei/aws/azure/volcano 固定顺序）；相关云存在 active 账号；CAS 空闲"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "status"
            value: "active"
      - entity_type: "CertLibraryInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "同步轮进入枚举阶段：按固定顺序遍历 certSyncClouds 并对每云调用 ActiveByCloud 取 active 账号"
- Output: "每个云×账号构成独立处理单元；轮摘要 CloudsScanned 只计有列举端口且账号读取成功的云数，AccountsScanned 累计枚举的 (cloud, account) 对数"
- State: "枚举阶段不写台账/映射（仅判定输入收集）"
- Side-effect: "对云侧仅只读列举调用"

## Outcome "empty-account-skip"
<!-- source: inferred -->
<!-- reasoning: journey Step 2b + Fact Table CERT_SYNC_JUDGE_FOUR_STATES（cert_sync_service.go:354-361，空实例集不产生判定条目也不记失败） -->
- Preconditions: "某云某 active 账号证书库无任何证书实例"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
    state_requirements:
      - description: "该账号证书库列举返回空实例集"
        prerequisite_entity: "CloudAccount"
- Input: "同步枚举并列举该账号"
- Output: "空枚举按跳过处理，不计失败、不记 errorReason，其余云与账号正常继续"
- State: "Listed 计数不增加；无导入条目产生"
- Side-effect: "none"

## Outcome "lister-gap-cloud-skip"
<!-- source: inferred -->
<!-- reasoning: Fact Table CERT_SYNC_LISTER_GAP_SKIP（cert_sync_service.go:274-279，lister==nil 能力缺口非故障静默跳过；module.go:239-242 生产装配当前仅火山注册列举端口） -->
- Preconditions: "某证书可达云未注册证书库列举端口（当前五云均未接入列举端口）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "未注册列举端口的云（非 volcano）"
    state_requirements:
      - description: "该云在同步服务列举端口表中无注册项"
        prerequisite_entity: "CloudAccount"
- Input: "同步轮遍历至该云"
- Output: "该云静默跳过（能力缺口非故障），不计失败不报错；其余云继续枚举"
- State: "CloudsScanned 不计入该云；无该云任何失败记录"
- Side-effect: "none"

## Journey Invariants
- 同步执行路径只入账不上线：不调用任何云写方法（只读纪律）
- 单实例/单云失败隔离，不中断其他云与账号

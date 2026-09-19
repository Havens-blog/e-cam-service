---
journey: "multi-account-shared-fs"
step: 4
step-action: "聚合层去重消费"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/multi-account-shared-fs/journey.md
---

# Contract: multi-account-shared-fs / Step 4: 聚合层去重消费

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "同一 fs_id 有三个账号的指标行并存;运营已登录且持有租户资产查看权限"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 3
        field_constraints:
          - field: "tenant_id"
            value: "等于当前会话租户"
      - entity_type: "NASMetric"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "fs_id"
            value: "相同 fs_id"
          - field: "capacity"
            value: "各账号值不同(可分辨代表行)"
- Input: "查看运营卡/Top 排行(账号视角聚合)"
- Output: "聚合按 fs_id 去重:取「日期 desc 再容量 desc」第一行代表该物理 fs,不双计、不跨账号求和/平均;Top 单条目的 account_id 为去重升序账号列表"
- State: "无状态变更(纯读)"
- Side-effect: "none"

## Outcome "representative-row-capacity-basis"
- Preconditions: "共享 fs 在不同账号下的容量口径不同(共享配额/共享文件系统视图),且各行日期或容量可分辨"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 3
        field_constraints:
          - field: "fs_id"
            value: "相同 fs_id"
          - field: "capacity"
            value: "各行容量值互不相同"
- Input: "查看聚合视图该 fs 的容量值"
- Output: "以最新日期行的厂商返回值为该物理 fs 的容量/用量口径(同日内取容量最大行,即「日期 desc 再容量 desc」第一行);不做跨账号求和或平均,避免「双计已除、口径仍高估」"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "account-id-list-dedup-asc"
<!-- source: inferred -->
<!-- reasoning: Fact Table NAS_TOP_AGG_REPRESENTATIVE(asset_nas_query.go:346-360):Top 项 account_id 为跨账号去重升序列表,T11 前端契约依赖该口径;多账号场景是该字段唯一非平凡路径 -->
- Preconditions: "同一 fs_id 被至少 3 个账号采集且请求窗口内各账号均有行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 3
      - entity_type: "NASMetric"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "请求 GET /assets/nas/top 查看该共享 fs 的条目"
- Output: "该 fs 条目的 account_id 字段为恰好覆盖全部采集账号的去重升序列表,无重复项"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants

- 任何聚合视图必须先按 fs_id 去重再计数,共享 fs 永不双计
- 共享 fs 的物理容量口径唯一来源 = 「日期 desc 再容量 desc」第一行的厂商返回值
- 聚合口径绝不跨账号容量求和或平均
- 趋势读取按账号隔离,聚合视图按 fs_id 去重,两语义互不混淆

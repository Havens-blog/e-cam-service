---
journey: "multi-account-shared-fs"
step: 2
step-action: "唯一键各行独立落库"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/multi-account-shared-fs/journey.md
---

# Contract: multi-account-shared-fs / Step 2: 唯一键各行独立落库

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "三账号采集结果就绪;ecam_nas_metric 唯一索引 (account_id, fs_id, date) 已建立"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 3
      - entity_type: "NASMetric"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "fs_id"
            value: "相同 fs_id(共享文件系统)"
          - field: "date"
            value: "同一日(今日)"
- Input: "三账号采集结果按 (account_id, fs_id, date) upsert 写入"
- Output: "三个账号同日各留一行,互不覆盖、互不合并;跨账号同 fs 并存语义成立"
- State: "ecam_nas_metric 同 fs 同日恰好三行,每行 account_id 各异且数值各自独立"
- Side-effect: "对 ecam_nas_metric 的批量 upsert 写入"

## Outcome "today-first-write-protected"
- Preconditions: "同一账号同日因手动/自动重叠被采集两次,今日行已首写;或当日内获得更准确的厂商聚合值尝试修正"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 1
        field_constraints:
          - field: "date"
            value: "今日"
          - field: "capacity"
            value: "已首写的任意合法值"
- Input: "第二次 upsert 同 (account_id, fs_id, 今日) 行(重复采集值或修正值)"
- Output: "今日行首写生效,第二次写入不覆盖首写值;仅补缺失行;修正发生在次日补采覆盖环节"
- State: "今日行所有字段与首写一致"
- Side-effect: "写入走 $setOnInsert 首写保护路径,唯一键命中时不修改任何字段"

## Outcome "next-day-backfill-account-isolated"
- Preconditions: "三账号昨日行均已落库,次日补采按账号逐个执行"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 3
        field_constraints:
          - field: "date"
            value: "昨日"
          - field: "fs_id"
            value: "相同 fs_id,三账号各一行"
- Input: "次日补采对三账号的昨日行执行覆盖更新"
- Output: "每账号仅覆盖自己的昨日行;补采完成后昨日行冻结,跨账号互不触碰"
- State: "三行昨日行各自被对应账号的补采值更新;任一行不被其他账号的写入影响"
- Side-effect: "对昨日行走覆盖更新路径(BulkUpsertMetrics)"

## Outcome "cross-account-no-overwrite"
<!-- source: inferred -->
<!-- reasoning: journey Invariant 第一条:唯一键含 account_id 使任何写入路径都不得让一个账号的行覆盖另一账号的行——这是本 journey High 定级的数据丢失风险本体;Fact Table NAS_METRIC_UNIQUE_KEY(nas_metric.go:60-72)确认唯一键设计,但写入语义需测试实锁 -->
- Preconditions: "账号 A 的写入与账号 B 的写入在同一物理 fs、同一日上交错执行(任意先后顺序)"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 2
        field_constraints:
          - field: "fs_id"
            value: "相同 fs_id"
          - field: "account_id"
            value: "两个不同账号"
- Input: "交错执行两账号对同 fs 同日的 upsert 写入"
- Output: "每账号的行始终保持本账号最新写入值;不存在任何一行被另一账号的写入覆盖、删除或合并"
- State: "两行并存且各自独立;行数恒为账号数,不随写入次数增长"
- Side-effect: "none"

## Journey Invariants

- 唯一键 (account_id, fs_id, date) 之下,任何写入路径都不得让一个账号的行覆盖/删除另一账号的行
- 首写生效仅作用今日行;昨日行冻结后任何账号/任何路径均不可变
- 同键二次写入幂等,行数不随重复写入增长

---
journey: "multi-account-shared-bucket"
step: 1
step-action: "三账号同名 bucket 同日各留一行"
generated: "2026-09-20"
skip_eval: true
sources:
  - docs/features/oss-ops-insight/testing/multi-account-shared-bucket/journey.md
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: multi-account-shared-bucket / Step 1: 三账号同名 bucket 同日各留一行

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "three-accounts-isolated-rows"
- Preconditions: "租户下 3 个云账号(account A/B/C)各自纳管同名 bucket shared-assets;(account_id, bucket_name, date) 唯一索引已建立;当日各键均无既有行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 3
        field_constraints:
          - field: "bucket_name"
            value: "三账号均纳管同名 bucket shared-assets"
      - entity_type: "OSSBucketAsset"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "执行器采集三账号下的 shared-assets bucket,结果按唯一键 upsert 落库"
- Output: "同日 shared-assets 在 account A/B/C 下各有一行,互不覆盖、互不合并;容量/对象数各自反映各账号视角的真实值"
- State: "ecam_oss_metric 新增 3 行,account_id 各不相同"
- Side-effect: "none"
- Invariants: "唯一键含 account_id:跨账号同名 bucket 永不合并"

## Outcome "concurrent-write-same-key"
<!-- source: inferred -->
<!-- reasoning: Journey Step 1b 定义并发写同名键;Fact Table OSS_METRIC_UNIQUE_KEY + OSS_FIRST_WRITE_SEMANTICS 保证唯一键约束下至多一行生效 -->
- Preconditions: "两个采集 goroutine 并发写同一 (account_id, bucket_name, date) 键"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "OSSBucketAsset"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "并发执行同键 upsert 写入"
- Output: "唯一键约束保证至多一行生效(首写生效);不产生重复行或脏数据;写入不报唯一键冲突错误"
- State: "ecam_oss_metric 该键恰有一行"
- Side-effect: "none"

## Outcome "single-account-failure-isolated"
<!-- source: inferred -->
<!-- reasoning: Fact Table OSS_CONCURRENCY_MODEL——单账号失败只影响自身;Journey Invariants 要求跨账号采集互不传染,需显式守护 -->
- Preconditions: "三账号中某一个账号的适配器调用失败,其余两账号正常"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 3
      - entity_type: "OSSBucketAsset"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "执行器采集三账号,其中一账号必然失败"
- Output: "仅失败账号计入 failures 明细;正常两账号的 shared-assets 行照常落库"
- State: "ecam_oss_metric 新增 2 行(正常账号),失败账号无行"
- Side-effect: "none"

## Journey Invariants

- 唯一键 (account_id, bucket_name, date) 全程有效:同键至多一行,跨账号同名 bucket 各留一行、永不合并
- 任一账号的失败只影响自身,绝不阻塞其他账号的采集继续

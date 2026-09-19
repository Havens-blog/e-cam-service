---
journey: "watermark-overview-top"
step: 1
step-action: "查看运营卡水位聚合"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/watermark-overview-top/journey.md
---

# Contract: watermark-overview-top / Step 1: 查看运营卡水位聚合

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "运营已登录且持有租户资产查看权限;租户内多个账号与多个 NAS 实例近 N 天有正常非零指标行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "tenant_id"
            value: "等于当前会话租户"
      - entity_type: "NASMetric"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "capacity"
            value: "大于 0"
- Input: "运营打开 NAS 列表页,查看顶部运营卡的总容量/已用容量/平均使用率"
- Output: "聚合按 fs_id 去重后计数:同一物理文件系统只计一次;多账号并存时按「日期 desc 再容量 desc」取第一行作为该物理 fs 的容量口径;不做跨账号容量求和或平均"
- State: "无状态变更(纯读聚合)"
- Side-effect: "none"

## Outcome "shared-fs-dedup"
- Preconditions: "同一 fs_id 有 3 个及以上账号各留一行指标"
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
            value: "相同 fs_id"
- Input: "查看运营卡与 Top"
- Output: "该物理 fs 只计一次(取「日期 desc 再容量 desc」第一行),总容量/Top 不因共享被双计"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "average-denominator-skip"
- Preconditions: "实例集合中存在无数据实例与 capacity=0 异常实例(zero_exception),同时存在正常非零实例"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 2
        field_constraints:
          - field: "capacity"
            value: "一行大于 0(正常),一行等于 0(zero_exception)"
- Input: "查看运营卡平均使用率"
- Output: "无数据实例跳过不参与分母(不记 0 拉低均值);capacity=0 行也不参与均值、作为异常单独可见"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "collect-failure-warning-priority"
- Preconditions: "某账号/厂商全部实例当日采集失败(执行器任务 Result 失败计数大于 0)"
  fixture_spec:
    entities:
      - entity_type: "CollectTask"
        min_count: 1
        field_constraints:
          - field: "type"
            value: "nas:collect_metrics"
          - field: "result.failures"
            value: "非空且 error_count 大于 0"
- Input: "查看运营卡"
- Output: "「采集失败→警示」优先于「无数据→0 占位」——显示警示而非 0 占位;仅任务成功执行且指标真实为 0/空才落入 0 占位分支"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants

- 任何聚合视图(运营卡/Top)必须先按 fs_id 去重再计数,共享 fs 永不双计
- 聚合口径以最新日期行的厂商返回值为准,绝不跨账号容量求和或平均
- 「警示」判定永远优先于「0 占位」;未知(采集失败)不得伪装成真零容量
- 平均使用率的分母永不包含无数据实例与 capacity=0 异常实例

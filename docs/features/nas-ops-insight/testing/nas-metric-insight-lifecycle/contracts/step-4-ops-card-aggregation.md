---
journey: "nas-metric-insight-lifecycle"
step: 4
step-action: "运营查看存储水位运营卡"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/nas-metric-insight-lifecycle/journey.md
---

# Contract: nas-metric-insight-lifecycle / Step 4: 运营查看存储水位运营卡

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "运营已登录且持有租户资产查看权限;租户内存在多个 NAS 实例且近 N 天有正常非零指标行;多账号并存时同一物理 fs 的多行可按「日期 desc 再容量 desc」取代表行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
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
      - entity_type: "CollectTask"
        min_count: 1
        field_constraints:
          - field: "type"
            value: "nas:collect_metrics"
- Input: "运营打开 NAS 列表页请求运营卡聚合(近 N 天口径)"
- Output: "运营卡显示总容量/已用容量/平均使用率;聚合按 fs_id 去重后计数,同一物理文件系统只计一次;平均使用率对无数据实例与 capacity=0 行跳过不参与分母"
- State: "无状态变更(纯读聚合)"
- Side-effect: "none"

## Outcome "collect-failure-warning-priority"
- Preconditions: "某账号/厂商全部实例当日采集失败(最近一次采集任务 Result 的失败计数大于 0);运营查看运营卡"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "CollectTask"
        min_count: 1
        field_constraints:
          - field: "type"
            value: "nas:collect_metrics"
          - field: "result.failures"
            value: "非空数组且 error_count 大于 0"
- Input: "运营打开 NAS 列表页查看运营卡"
- Output: "显示采集失败警示而非纯空或 0 占位——「采集失败→警示」优先于「无数据→0 占位」判定"
- State: "无状态变更;警示状态由最近一次任务 Result 派生"
- Side-effect: "none"

## Outcome "zero-or-no-data-placeholder"
<!-- source: inferred -->
<!-- reasoning: journey Step 4b 的判定分支另一侧:仅任务成功执行且指标真实为 0/空时才落入 0 占位分支;Fact Table NAS_RESULT_KEYS 表明失败与空数据在 Result 上可分辨,两分支前置互斥 -->
- Preconditions: "最近一次采集任务成功执行且 Result 失败计数为 0;实例指标真实为空或全为零容量异常行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "CollectTask"
        min_count: 1
        field_constraints:
          - field: "type"
            value: "nas:collect_metrics"
          - field: "result.failures"
            value: "空数组(无失败)"
- Input: "运营打开 NAS 列表页查看运营卡"
- Output: "显示 0 占位/空数据态而非失败警示;若存在 zero_exception 行则按异常标注单独可见"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants

- 任何聚合视图(运营卡/Top)必须先按 fs_id 去重再计数,共享 fs 永不双计
- 聚合口径以最新日期行的厂商返回值为准,绝不跨账号容量求和或平均
- 「警示」判定永远优先于「0 占位」;未知(采集失败)不得伪装成真零容量
- 平均使用率的分母永不包含无数据实例与 capacity=0 异常实例
- NAS 界面数据一律以 `ecam_nas_metric` 指标表为唯一来源

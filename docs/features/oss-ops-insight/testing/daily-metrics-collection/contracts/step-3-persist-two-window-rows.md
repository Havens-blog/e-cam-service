---
journey: "daily-metrics-collection"
step: 3
step-action: "指标行落库(首写生效 + 补采覆盖)"
generated: "2026-09-20"
skip_eval: true
sources:
  - docs/features/oss-ops-insight/testing/daily-metrics-collection/journey.md
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: daily-metrics-collection / Step 3: 指标行落库(首写生效 + 补采覆盖)

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "today-first-write-yesterday-overwrite"
- Preconditions: "(account_id, bucket_name, date) 唯一索引已建立;今日行不存在;昨日行存在且为 00:10 初态"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "OSSMetricRow"
        min_count: 1
        field_constraints:
          - field: "date"
            value: "昨日"
          - field: "storage_size"
            value: "凌晨初态值"
- Input: "执行器 upsert 写入:今日行走首写生效写入,昨日行覆盖更新为厂商当日最终聚合值"
- Output: "写入成功;昨日行从初态被更新为日末态快照;今日行保持首次写入值;任务 Result metrics_total 相应增加"
- State: "ecam_oss_metric 中昨日行为日末态,今日行为首写值;跨账号同 bucket 名各留一行"
- Side-effect: "对 mongo ecam_oss_metric 的批量写"
- Invariants: "今日行当日内可变(首写生效);昨日行次日补采后冻结不可变"

## Outcome "today-row-never-overwritten"
<!-- source: inferred -->
<!-- reasoning: Journey Step 3b 定义同日重跑不覆盖今日行;Fact Table OSS_FIRST_WRITE_SEMANTICS(dao/oss_metric.go:139-157)BulkInsertIfAbsent $setOnInsert 命中已存在行不修改任何字段 -->
- Preconditions: "今日行已存在(首写已完成);执行器因回填/手动触发再次写入同日值"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 1
        field_constraints:
          - field: "date"
            value: "今日"
- Input: "对同 (account_id, bucket_name, 今日) 键再次执行首写生效写入"
- Output: "写入被幂等吸收,不报错;今日行保持首写值不被覆盖;仅补当日缺失行"
- State: "今日行内容不变;缺失的其他行被补齐"
- Side-effect: "none"

## Outcome "yesterday-row-frozen-after-backfill"
<!-- source: inferred -->
<!-- reasoning: Journey Step 3c 定义昨日行补采后冻结;两窗口语义不互相渗透,Journey Invariants 显式要求 -->
- Preconditions: "昨日行已在次日补采中覆盖更新为日末态(冻结窗口以 Asia/Shanghai 自然日为准)"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 1
        field_constraints:
          - field: "date"
            value: "昨日"
          - field: "storage_size"
            value: "日末态聚合值"
- Input: "当日晚些时候再次触发包含昨日区间的写入"
- Output: "写入路径对昨日行执行覆盖更新(执行器语义允许重采),但重采值与日末态一致时数据不变;跨日之后昨日行不再被任何今日写入触碰"
- State: "昨日行保持日末态,不回退为初态;今日行独立不受影响"
- Side-effect: "none"

## Journey Invariants

- 今日行当日内可变(首写生效);昨日行次日补采后冻结不可变;两窗口语义不互相渗透
- 唯一键 (account_id, bucket_name, date) 全程有效:同键至多一行,跨账号同名 bucket 各留一行

---
journey: "daily-metrics-collection"
step: 3
step-action: "指标行落库(首写生效+补采覆盖)"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/daily-metrics-collection/journey.md
---

# Contract: daily-metrics-collection / Step 3: 指标行落库(首写生效+补采覆盖)

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "采集产出有效指标行;ecam_nas_metric 唯一键 (account_id, fs_id, date) 上无对应行的行可插入、已有昨日行的行可覆盖"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "NASInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
      - entity_type: "NASMetric"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "date"
            value: "昨日(待补采覆盖的初态行)"
- Input: "执行器按唯一键 (account_id, fs_id, date) upsert 写入采集结果:今日行走首写保护路径,昨日行走覆盖更新路径"
- Output: "昨日行从 00:10 初态被更新为厂商当日最终聚合值;今日行保持首次写入值;跨账号同 fs 各留一行"
- State: "ecam_nas_metric 中昨日行 capacity/used_capacity/qc_status 被覆盖更新;今日行为新插入;每「(account_id, fs_id, date)」至多一行"
- Side-effect: "对 ecam_nas_metric 的批量写(BulkUpsertMetrics/BulkInsertIfAbsent 两条路径)"

## Outcome "today-first-write-protected"
- Preconditions: "今日行已存在(首写已完成),执行器因回填/手动触发再次写入同日值"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 1
        field_constraints:
          - field: "date"
            value: "今日"
          - field: "capacity"
            value: "任意已首写的合法值"
- Input: "upsert 同 (account_id, fs_id, 今日) 的新值"
- Output: "今日行保持首写值不被覆盖,仅补当日缺失的其他行"
- State: "今日行所有字段与首写时一致,无任何字段被修改"
- Side-effect: "写入走 $setOnInsert 路径,唯一键命中时不修改任何字段"

## Outcome "yesterday-frozen-after-backfill"
- Preconditions: "昨日行已在次日补采中覆盖更新为日末态"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 1
        field_constraints:
          - field: "date"
            value: "昨日"
          - field: "capacity"
            value: "日末态快照值"
- Input: "当日晚些时候再次触发包含昨日区间的写入"
- Output: "昨日行跨日不可变,重采不再触碰;冻结窗口以 Asia/Shanghai 自然日为准"
- State: "昨日行字段保持日末态不变;同批仅新增今日行"
- Side-effect: "none"

## Outcome "out-of-range-batch-rejected"
<!-- source: inferred -->
<!-- reasoning: Fact Table NAS_QC_GATE(nas_metric.go:120-138):批量写入任一行越出 [1MB,1PB] 数量级则整批拒绝,不良行不得落库,错误携带 fs_id;补采覆盖路径与首写路径同门禁 -->
- Preconditions: "批量写入中任一行换算后 capacity 非零且越出 [1MB, 1PB] 数量级区间"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 1
        field_constraints:
          - field: "capacity"
            value: "非零且越出 [1MB, 1PB] 数量级(字节直写 GB 字段的单位 bug 形态)"
- Input: "执行器提交含越界行的批量 upsert"
- Output: "整批拒绝并报错,错误携带 fs_id 供失败归因;该批全部行不落库"
- State: "ecam_nas_metric 不新增或更新任何该批行;执行器失败计数累加"
- Side-effect: "none"

## Outcome "zero-capacity-zero-exception"
<!-- source: inferred -->
<!-- reasoning: Fact Table NAS_QC_GATE(nas_metric.go:95-97):capacity=0 例外放行并强制 qc_status=zero_exception,不拦截不跳过(NAS 不继承 CDN 全零过滤);华为/AWS 实盘现状使该边界成为主链必经路径 -->
- Preconditions: "厂商返回该实例当日 capacity=0(华为/AWS 实盘现状)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "NASInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "厂商监控 API 返回 capacity=0"
        prerequisite_entity: "NASInstance"
- Input: "采集写入该实例当日行(capacity=0)"
- Output: "行标记 qc_status=zero_exception 后正常落库,不拦截不跳过"
- State: "ecam_nas_metric 新增 capacity=0 且 qc_status=zero_exception 的行;读取侧映射 data_status=zero_exception"
- Side-effect: "none"

## Journey Invariants

- 每日每「(account_id, fs_id, date)」至多一行;今日行当日内可变(首写生效);昨日行次日补采后冻结不可变;两窗口语义不互相渗透
- 所有落库容量字段以 GB 为唯一口径;非零 capacity 越出 [1MB, 1PB] 整批拒绝
- capacity=0 行强制打 zero_exception 后落库可见,永不继承「全零跳过」类过滤
- utilization 永不落库

---
journey: "disk-metrics-daily-collection"
step: 6
step-action: "用户查看单盘趋势"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/disk-metrics-daily-collection/journey.md
skip_eval: true
---

# Contract: disk-metrics-daily-collection / Step 6: 用户查看单盘趋势

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "用户持有租户有效鉴权凭证;指定 account_id 属于当前租户账号集合;ecam_disk_metric 已有该盘近 N 天指标行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "当前租户"
      - entity_type: "DiskMetric"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "disk_id"
            value: "同一被查询盘的 ID"
- Input: "用户在 Disk 抽屉「监控」tab 打开趋势图,或调用 GET /assets/disk/metrics?disk_id=&account_id=&days=(days 默认 30,上限 90)"
- Output: "200 响应:days 数组按日期升序含逐日 usage_percent/usage_scope/iops/throughput/data_status/qc_status;latest(最新一天)与 average(近 N 天均值)两类值;数据一律来自 ecam_disk_metric 指标表,资产表快照数值不展示"
- State: "只读,无状态变更"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效鉴权凭证(token 缺失或过期)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "请求方未认证"
        prerequisite_entity: "CloudAccount"
- Input: "不带有效凭证调用 GET /assets/disk/metrics"
- Output: "401 语义的认证失败响应,响应体含认证错误信息,不泄露敏感数据,不进入业务逻辑"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "missing-day-annotated"
- Preconditions: "请求趋势的 days 区间内某天无指标行(如启用日之前、厂商当日故障)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "DiskMetric"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "请求窗口内存在至少一个无指标行的日期"
        prerequisite_entity: "DiskMetric"
- Input: "调用 GET /assets/disk/metrics 查询含缺失日的区间"
- Output: "缺失日以 data_status=missing 标注且各指标值为 null,不填假值;近 N 天均值仅基于实际存在日计算;缺失日标注与写入侧 qc_status 闭环关联"
- State: "只读,无状态变更"
- Side-effect: "none"

## Journey Invariants
- 唯一键 (account_id, disk_id, date) 恒成立:任何时刻同一键至多一行,多账号同 disk_id 并存各留一行,互不覆盖
- 同日首写生效:当日已存在的行永不被同日写入覆盖,昨日行仅由次日补采覆盖更新
- 失败隔离:任一厂商/账号的采集失败不阻塞其他厂商/账号,失败必须可观测(计数/日志/告警),不允许静默吞错
- 数据来源唯一:Disk 界面(趋势/Top/运营卡)一律以 ecam_disk_metric 为唯一数据来源,资产表快照数值不得在指标界面展示
- 账号级互斥 + disk 有界并发全程生效,同一账号不会被并发重复采集

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "CloudAccount"
      min_count: 1
      field_constraints:
        - field: "tenant_id"
          value: "当前租户"
    - entity_type: "DiskMetric"
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "CloudAccount"
  state_requirements:
    - description: "success 分支要求近 N 天多日行;missing-day 分支要求窗口内存在缺失日;unauthorized 分支仅要求未认证请求"
      prerequisite_entity: "DiskMetric"
```

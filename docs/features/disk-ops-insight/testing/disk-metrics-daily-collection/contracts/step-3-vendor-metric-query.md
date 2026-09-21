---
journey: "disk-metrics-daily-collection"
step: 3
step-action: "逐厂商调用磁盘指标查询"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/disk-metrics-daily-collection/journey.md
skip_eval: true
---

# Contract: disk-metrics-daily-collection / Step 3: 逐厂商调用磁盘指标查询

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "账号在 ecam_instance 中有 ≥1 个 disk 实例,且该厂商适配器实现了 DiskMetricQuerier 可选能力"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "必达厂商 aliyun/huawei/aws 之一"
      - entity_type: "DiskInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "region"
            value: "实例 attributes 中的非空地域标识"
- Input: "对账号下每个磁盘按实例真实 region 调用 GetDiskMetrics(ctx, diskID, diskName, region, startDate, endDate),区间默认[昨日,今日]"
- Output: "返回按日归一化指标:usage_percent 统一 0~100 百分比口径(语义由 usage_scope 标注:instance_level/busy_share/cloud_disk_level),iops 次/秒,throughput MB/s(byte/s 到 MB/s 归一在适配器边界完成);按地域性资源语义执行,不做全局 region 推断"
- State: "执行器回填 AccountID/Provider,透传适配器的 qc_status/usage_scope 标注不篡改"
- Side-effect: "调用厂商监控 API(网络访问)"

## Outcome "metric-unsupported-info"
- Preconditions: "厂商为尽力而为项(tencent/volcengine),监控侧不存在磁盘指标(探测不支持,适配器未实现 DiskMetricQuerier)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "tencent 或 volcengine"
      - entity_type: "DiskInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "采集执行器尝试调用该厂商的 DiskMetricQuerier"
- Output: "记录 INFO 日志并整体返回空结果,不计失败、不触发告警;结果汇总 no_metric_support 列出该厂商"
- State: "不写入该厂商任何指标行"
- Side-effect: "none"

## Outcome "adapter-call-failure"
- Preconditions: "某厂商监控 API 调用返回错误(网络/鉴权/限流等)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "DiskInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "采集执行器调用该厂商的 DiskMetricQuerier"
- Output: "记录 ERROR 日志;该盘计入 failed_disks,失败明细(provider/account_id/error_count/last_error)汇入 Result 的 failures 列表;该盘仅返回自身空结果"
- State: "该盘无指标行落库;其余厂商/账号的磁盘采集不受影响"
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
        - field: "provider"
          value: "按 Outcome 分支:必达厂商或尽力而为厂商"
    - entity_type: "DiskInstance"
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "CloudAccount"
      field_constraints:
        - field: "region"
          value: "实例 attributes 中的地域标识"
```

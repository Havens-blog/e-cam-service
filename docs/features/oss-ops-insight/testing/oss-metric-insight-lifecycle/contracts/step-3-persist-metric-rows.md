---
journey: "oss-metric-insight-lifecycle"
step: 3
step-action: "指标行落库(唯一键 upsert 首写生效)"
generated: "2026-09-20"
skip_eval: true
sources:
  - docs/features/oss-ops-insight/testing/oss-metric-insight-lifecycle/journey.md
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: oss-metric-insight-lifecycle / Step 3: 指标行落库(唯一键 upsert 首写生效)

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "first-write-and-backfill-upsert"
- Preconditions: "ecam_oss_metric collection 已建立 (account_id, bucket_name, date) 唯一索引;今日行不存在(首写),昨日行已存在(初态,待补采覆盖)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "OSSBucketAsset"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
      - entity_type: "OSSMetricRow"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "OSSBucketAsset"
        field_constraints:
          - field: "date"
            value: "昨日(Asia/Shanghai)"
          - field: "storage_size"
            value: "凌晨初态值"
    state_requirements:
      - description: "ecam_oss_metric 的 (account_id, bucket_name, date) 唯一索引已建立"
        prerequisite_entity: "OSSMetricRow"
- Input: "执行器写入采集结果:今日行走首写生效写入,昨日行走覆盖更新写入(字段含 bucket_name/date/storage_size(GB)/object_count/qc_status)"
- Output: "写入成功;今日行新增生效,昨日行被更新为日末态;任务 Result 的 metrics_total 增加相应条数"
- State: "ecam_oss_metric 中今日行首写生效(同日重复写不覆盖);昨日行更新为日末态;同 bucket_name 跨账号各留一行"
- Side-effect: "对 mongo ecam_oss_metric 的批量写(BulkWrite upsert)"
- Invariants: "唯一键 (account_id, bucket_name, date) 全程有效;今日行首写生效、昨日行次日补采覆盖后冻结"

## Outcome "zero-exception-row"
<!-- source: inferred -->
<!-- reasoning: Journey Step 3b 定义 storage_size=0 异常行;Fact Table OSS_QC_ZERO_EXCEPTION(dao/oss_metric.go:88-100)明确零值例外放行并强制打 qc_status=zero_exception -->
- Preconditions: "厂商返回某 bucket 当日容量为 0(非正常空桶场景);该键今日行尚不存在"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "OSSBucketAsset"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "采集结果含 storage_size=0 的今日行,执行落库写入"
- Output: "写入成功不被拒绝;落库行 qc_status=zero_exception;读取侧原样暴露 qc_status 并映射 data_status=zero_exception,前端可分辨「容量为 0 是异常」"
- State: "ecam_oss_metric 新增一行 storage_size=0 且 qc_status=zero_exception 的指标行"
- Side-effect: "none"

## Outcome "out-of-range-rejected"
<!-- source: inferred -->
<!-- reasoning: Journey Step 3c 定义容量字节非零越界;Fact Table OSS_RANGE_GATE(dao/oss_metric.go:74-99)明确非零越出 [1MB,1PB] 拒绝写入且错误携带 bucket_name/date -->
- Preconditions: "厂商返回的容量字节换算后为非零值且越出 [1MB, 1PB] 数量级门禁(字节直写 GB 字段的单位 bug 形态)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "OSSBucketAsset"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "采集结果含非零越界 storage_size 的行,尝试落库写入"
- Output: "该行被拒绝不写入;错误信息携带 bucket_name 与 date,失败可观测(ERROR 日志/失败明细);批量写时任一行越界整批拒绝"
- State: "ecam_oss_metric 不产生任何越界脏行,既有数据不受污染"
- Side-effect: "none"

## Journey Invariants

- 唯一键 (account_id, bucket_name, date) 全程有效;今日行当日内首写生效,昨日行次日补采后冻结
- storage_size=0 异常行放行落库可见(zero_exception 打标),非零越界行拒绝写入
- 落库每行容量单位为 GB(二进制 GiB),数量级可反向验证

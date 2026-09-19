---
journey: "nas-metric-insight-lifecycle"
step: 2
step-action: "厂商指标按 GB 口径落库"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/nas-metric-insight-lifecycle/journey.md
---

# Contract: nas-metric-insight-lifecycle / Step 2: 厂商指标按 GB 口径落库

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "活跃账号下的 NAS 实例适配器可用(必达厂商 aliyun/huawei/aws 至少一家);适配器返回有效的厂商原始字节值;指标表唯一键 (account_id, fs_id, date) 上无今日行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "NASInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "region"
            value: "任意合法地域字符串"
      - entity_type: "CollectTask"
        min_count: 1
        field_constraints:
          - field: "type"
            value: "nas:collect_metrics"
- Input: "执行器按实例所在地域调用厂商监控 API,采集区间 [昨日, 今日],厂商字节值在采集边界换算为 GB(二进制 GiB)"
- Output: "每个「活跃账号 × NAS 实例 × 当日」各落一行 capacity/used_capacity(GB) 记录;utilization 不落库;任务 Result 的 metrics_total 反映落库行数"
- State: "ecam_nas_metric 新增今日行(首写生效);昨日行以覆盖更新路径刷新为厂商日末聚合值;写入行数量级落在 [1MB, 1PB] 区间"
- Side-effect: "对 ecam_nas_metric 集合执行批量 upsert 写入(今日行走仅插入保护路径)"

## Outcome "vendor-api-failure-isolated"
- Preconditions: "aliyun/huawei/aws 中某一家监控 API 宕机或鉴权失效,其余厂商适配器正常"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
      - entity_type: "NASInstance"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "其中一家厂商适配器被注入调用失败,其余厂商正常返回数据"
        prerequisite_entity: "NASInstance"
- Input: "执行器遍历实例采集,命中失败厂商的实例"
- Output: "该厂商仅返回自身空结果;其余厂商实例照常产出指标行;任务 Result 的 failures 数组含该厂商的失败计数与末次错误"
- State: "失败厂商实例不落库指标行;其余厂商指标行正常落库;全流程任务不因此整体失败"
- Side-effect: "失败路径打 ERROR 级日志并携带错误字段"

## Outcome "zero-capacity-zero-exception"
- Preconditions: "华为/AWS 实例厂商返回容量为 0(实盘现状),实例属于活跃账号"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "NASInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "厂商监控 API 对该实例返回 capacity=0"
        prerequisite_entity: "NASInstance"
- Input: "采集写入该实例当日行(capacity=0)"
- Output: "不拦截不跳过,行标记 qc_status=zero_exception 后正常落库;读取侧该日 data_status=zero_exception、utilization 记 null"
- State: "ecam_nas_metric 新增 capacity=0 且 qc_status=zero_exception 的今日行"
- Side-effect: "无额外告警(零容量是可分辨的异常标注而非失败)"

## Outcome "capacity-out-of-range-rejected"
<!-- source: inferred -->
<!-- reasoning: Fact Table NAS_QC_GATE(nas_metric.go:93-105):非零 capacity 越出 [1MB,1PB] 即「字节直写 GB 字段」的单位 bug 形态,门禁整批拒绝,错误携带 fs_id/date;推断该边界为写入路径必须锁死的回归防线 -->
- Preconditions: "某适配器返回的换算后 capacity 非零且越出 [1MB, 1PB] 数量级区间(单位换算缺陷形态)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "NASInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "厂商返回值经采集边界换算后仍越出数量级门禁区间"
        prerequisite_entity: "NASInstance"
- Input: "执行器提交含越界 capacity 行的批量写入"
- Output: "整批拒绝并报错,错误信息携带 fs_id 与 date;该批不良行全部不得落库"
- State: "ecam_nas_metric 不新增任何越界行;执行器失败计数累加,末次错误进入任务 Result"
- Side-effect: "none"

## Journey Invariants

- 每日每「(account_id, fs_id, date)」至多一行;同日首写生效仅保护今日行,昨日行由次日补采覆盖更新后冻结跨日不可变(Asia/Shanghai 自然日)
- 所有落库容量字段以 GB 为唯一口径,字节到 GB 的换算只发生在采集边界,原始字节永不直写 GB 字段
- utilization 永不落库,任何读取路径均由 capacity/used 现场派生,且 capacity=0 时为 null(无 NaN、无 panic)
- 任何单厂商/单实例采集失败不得阻塞其他厂商/实例的采集与读取(尽力而为语义),且失败必须可观测(失败计数/末次错误/前端空态区分)
- capacity=0 的异常行永远落库可见,不受任何「全零跳过」类过滤影响

---
journey: "multicloud-cert-replacement"
step: 2
step-action: "确认变更清单并进入分批执行"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/multicloud-cert-replacement/journey.md
skip_eval: true
---

# Contract: multicloud-cert-replacement / Step 2: 确认变更清单并进入分批执行

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "变更单处于 pending_confirm 且已设置条目；快照仍新鲜（未超扫描时效阈值）；引用一致性复核与快照一致；操作者为 ops_engineer 有效会话"
  fixture_spec:
    entities:
      - entity_type: "ChangeOrder"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "pending_confirm"
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "pending"
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done 且时效新鲜"
- Input: "POST /api/v1/certs/changes/:id/confirm 携带分批配置（是否分批/批量/比例），随后 POST /api/v1/certs/changes/:id/execute 启动执行"
- Output: "确认受理：批次分配落定（首批规模不超过总数一半口径，比例校验通过）；执行受理：当前批 pending 条目进入 running，每批对三云引用走与 aliyun/tencent 相同的两段式编排（先上传段、后绑定段）"
- State: "变更单迁移 executing；条目 running"
- Side-effect: "经部署器发起云上传/绑定调用（每云限流与有界退避覆盖）"

## Outcome "batch-gate-blocking"
<!-- 事实对齐：confirm-batch 门禁要求上一批全部 success-or-skipped 且已达验证确认（execute_service.go:916-938）；限流条目先自动退避重试（:682-698）。journey 的"失败批次可重试续跑"在实现中表现为：批内限流自动重试 + 已完成批次结果保留不回退，硬失败则阻断前滚。 -->
- Preconditions: "分批执行中批内存在条目未全部成功：限流条目尚在自动退避重试，或存在硬失败条目致上一批不满足全部 success-or-skipped"
  fixture_spec:
    entities:
      - entity_type: "ChangeOrder"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "executing（多批，非最后一批）"
      - entity_type: "ChangeItem"
        min_count: 2
        field_constraints:
          - field: "status"
            value: "混合：限流重试中（rate_limited）或已失败（failed）与已完成（success）并存"
- Input: "运维人员观察执行进度并尝试 POST /api/v1/certs/changes/:id/confirm-batch 续跑"
- Output: "409 BATCH_NOT_CONFIRMABLE：门禁拦截不自动前滚；限流条目由系统按有界退避自动重试（重试成功不留失败残留）；已完成批次结果保留，不整单回退、不重复执行已完成段"
- State: "失败条目保持 failed；变更单保持 executing（分批暂停语义），不进入下一批"
- Side-effect: "限流重试期间存在云 API 重试调用；门禁拒绝本身无云调用"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效会话"
  fixture_spec:
    entities:
      - entity_type: "ChangeOrder"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "pending_confirm"
    state_requirements:
      - description: "无有效会话"
        prerequisite_entity: "ChangeOrder"
- Input: "POST /api/v1/certs/changes/:id/confirm 或 POST /api/v1/certs/changes/:id/execute 无有效会话调用"
- Output: "HTTP 401 全局认证失败文案；不进入角色判定（403 属另一层级，不在此 Outcome）"
- State: "变更单状态不变，批次不分配"
- Side-effect: "none"

## Journey Invariants
- 三云替换语义与 aliyun/tencent 完全同构：失败状态、回滚、清理走同一状态机，不引入云特定的新验证/回滚机制
- 两段式顺序不可逆：UploadCert 成功前不得 BindResource；绑定失败必经 CleanupOrphan 补偿，不允许跳过补偿直接重试
- 云证书 ID 空间互斥（SCM ID / ACM ARN / KV 引用），映射唯一性天然成立，禁止跨云混用或猜测归一
- 变更清单可执行性唯一受 K8s 管理权/托管资源约束，三云不因云差异降级为 discovery-only

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeOrder"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "success: pending_confirm；batch-gate: executing 多批；unauthorized: pending_confirm"
    - entity_type: "ChangeItem"
      min_count: 2
      field_constraints:
        - field: "status"
          value: "success: pending；batch-gate: success/failed/rate_limited 混合"
    - entity_type: "ScanSnapshot"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "done"
```

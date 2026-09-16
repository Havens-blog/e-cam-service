---
journey: "rollback-restore-old-cert"
step: 4
step-action: "验证窗口确认线上指纹恢复旧证书"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/rollback-restore-old-cert/journey.md
skip_eval: true
---

# Contract: rollback-restore-old-cert / Step 4: 验证窗口确认线上指纹恢复旧证书

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "probe-consistent-with-old"
<!-- 事实对齐：回滚闭环由绑定结果与条目状态判定（rollback_service.go 逐条回滚 + 收敛），无独立回滚验证窗口；拨测为只读观测，线上指纹与台账任一属主证书一致即分类 consistent（probe_service.go:304-348）。 -->
- Preconditions: "回滚条目已收敛 rolled_back（回滚状态由绑定结果决定，与拨测解耦）；目标域可建立 TLS 连接"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "rolled_back"
      - entity_type: "ProbeResult"
        min_count: 1
        field_constraints:
          - field: "classification"
            value: "consistent"
- Input: "运维人员等待系统对目标域名执行 TLS 拨测"
- Output: "ProbeDomains 拨测线上指纹与旧证书台账指纹一致 → 拨测分类 consistent；资源回到替换前状态，回滚闭环可观测（条目 rolled_back、变更单 rolled_back/partial_completed）"
- State: "ProbeResult 落库 consistent；回滚终态不变"
- Side-effect: "TLS 拨测（443 端口只读握手）"

## Outcome "cleanup-queue-race-protected"
- Preconditions: "回滚发起时目标旧证书恰在清理队列中（映射已标记 orphan）但尚未被删除"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "orphan（旧证书）"
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "success（回滚执行中）"
- Input: "系统协调回滚与清理"
- Output: "回滚完成后旧证书获得保护期（protectUntil = 当前时间 + 默认 7 天），清理消费按保护期跳过保留、不删除；不出现回滚成功但旧证书已删的矛盾终态（若旧证书已被删除，则预检以 409 回滚目标无效阻断，不进入回滚）"
- State: "旧证书台账 protectUntil 置位；旧证书映射保持 orphan 留队列（保护期外再清）"
- Side-effect: "none"

## Outcome "probe-lag-converges"
- Preconditions: "反绑已完成但 CDN/边缘节点指纹同步滞后，窗口内前几轮拨测指纹暂未回到旧证书"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "rolled_back"
      - entity_type: "ProbeResult"
        min_count: 1
        field_constraints:
          - field: "classification"
            value: "diff（滞后期间）"
- Input: "系统在验证窗口内继续周期拨测"
- Output: "后续轮次收敛为与旧证书一致；滞后期间拨测差异仅记 diff 分类与常规告警，不改变回滚终态——不因单次拨测不一致误判回滚失败"
- State: "ProbeResult 逐轮落库；回滚终态（rolled_back）不变"
- Side-effect: "周期拨测调用"

## Journey Invariants
- 回滚终态唯一（资源引用旧证书），与清理/orphan 状态互斥协调，不允许出现引用已删除证书的终态
- 回滚状态由绑定结果决定，拨测为只读观测、不反向改变回滚终态
- 回滚完成的旧证书获得保护期保护，免于清理队列误删

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeItem"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "consistent/lag 分支: rolled_back；race 分支: success（回滚执行中）"
    - entity_type: "CloudCertMapping"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "race 分支: orphan（旧证书）"
    - entity_type: "ProbeResult"
      min_count: 1
```

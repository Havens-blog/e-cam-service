---
journey: "multicloud-cert-replacement"
step: 6
step-action: "验证窗口拨测确认线上指纹"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/multicloud-cert-replacement/journey.md
skip_eval: true
---

# Contract: multicloud-cert-replacement / Step 6: 验证窗口拨测确认线上指纹

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "变更单最终批执行完成并进入验证窗口（窗口到期时间=进入时间+默认 24 小时）；验证预期已封存（期望指纹=新证书指纹、域集=旧证书 SAN 减豁免）；目标域可建立 TLS 连接"
  fixture_spec:
    entities:
      - entity_type: "ChangeOrder"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "verifying"
      - entity_type: "ProbeResult"
        min_count: 2
        field_constraints:
          - field: "classification"
            value: "与期望新证书指纹一致的连续记录"
- Input: "运维人员在验证窗口内等待系统按周期（默认 10 分钟）对目标域名执行 TLS 拨测（ProbeDomains，按域名云无关）"
- Output: "拨测取得线上叶子证书指纹，与期望新证书指纹连续 VerifyConfirmProbes 轮（默认 2 轮）一致 → 窗口达成，变更条目标记验证通过，最终批变更单收敛 completed"
- State: "拨测结果逐轮落库；窗口关闭、变更单 completed；拨测分类记为一致"
- Side-effect: "TLS 拨测（443 端口只读握手，无应用层请求）"

## Outcome "probe-mismatch-unmet"
- Preconditions: "绑定已完成但窗口内拨测线上指纹不等于期望新证书指纹（如 CDN 边缘未同步），连续一致轮次不足确认阈值"
  fixture_spec:
    entities:
      - entity_type: "ChangeOrder"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "verifying"
      - entity_type: "ProbeResult"
        min_count: 1
        field_constraints:
          - field: "classification"
            value: "变更关联不一致（关联在途验证单）"
- Input: "系统在验证窗口内按周期继续拨测判定"
- Output: "不标记验证通过：相关域拨测分类为变更关联不一致并发布变更关联告警（仅对新进入该分类的域）；窗口到期仍未达成 → 变更单收敛 partial_completed 并持久化未达成域清单，不因单次拨测失败误判成功"
- State: "窗口保持 verifying 直至到期；到期收敛后（最终批）partial_completed；非最终批则回 executing+paused 等待人工决策"
- Side-effect: "周期拨测调用 + 告警发布（同域重复告警去重）"

## Journey Invariants
- 三云替换语义与 aliyun/tencent 完全同构：失败状态、回滚、清理走同一状态机
- 验证判定以连续多轮一致为准，不因单次拨测成功/失败误判
- 云证书 ID 空间互斥；映射唯一性保持

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeOrder"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "verifying"
    - entity_type: "ProbeResult"
      min_count: 2
      field_constraints:
        - field: "classification"
          value: "success 分支为与期望一致的连续记录；mismatch 分支为变更关联不一致"
```

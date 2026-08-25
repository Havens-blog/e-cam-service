---
journey: "no-snapshot-guidance"
step: 2
step-action: "按引导触发引用扫描"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/no-snapshot-guidance/journey.md
skip_eval: true
---

# Contract: no-snapshot-guidance / Step 2: 按引导触发引用扫描

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "当前无扫描在途；至少一个云账号可发起引用扫描；操作者具备 OpsEngineer 角色"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
    state_requirements:
      - description: "无 running 状态的扫描快照在途"
        prerequisite_entity: "ScanSnapshot"
- Input: "运维人员点击引导中的触发扫描操作（立即扫描触发端点，OpsEngineer 权限）"
- Output: "系统发起一次引用扫描，产生 running 状态的快照；引导切换为轮询等待态"
- State: "新增 running 状态 ScanSnapshot；引用开始随扫描进度落库"
- Side-effect: "扫描异步编排启动，逐云逐账号调用云 API"

## Outcome "scan-already-running"
<!-- source: inferred -->
<!-- reasoning: Fact Table SCAN_TRIGGER_ENDPOINT（reference_handler.go:31-35,163-165 + response.go:113-118）：立即扫描端点对在途扫描防重返回 409 SCAN_IN_PROGRESS 附快照信息；对应 journey 边缘场景"他人已触发扫描在途"的服务端口径（不重复触发新扫描） -->
- Preconditions: "一次引用扫描已在途（最近快照 status=running，由他人或前次操作触发）"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "运维人员在扫描在途时再次触发扫描操作"
- Output: "409 冲突响应，错误码 SCAN_IN_PROGRESS，附在途快照信息；不重复编排新扫描，引导进入对既有 running 快照的轮询等待"
- State: "不新增第二条 running 快照"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效登录会话（未认证）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "请求方未认证"
        prerequisite_entity: "CloudAccount"
- Input: "不带有效凭证触发扫描操作"
- Output: "401 语义的认证失败响应，不发起扫描"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 引导流程绝不依赖单次长请求同步返回扫描结果
- 扫描在途时不重复触发新扫描（在途即等待）

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "CloudAccount"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "active"
    - entity_type: "ScanSnapshot"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "running"   # scan-already-running 分支
  state_requirements:
    - description: "success 分支无 running 快照在途；already-running 分支有在途快照"
      prerequisite_entity: "ScanSnapshot"
```

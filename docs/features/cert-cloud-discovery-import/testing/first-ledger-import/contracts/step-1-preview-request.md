---
journey: "first-ledger-import"
step: 1
step-action: "发起云端发现预览"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/first-ledger-import/journey.md
skip_eval: true
---

# Contract: first-ledger-import / Step 1: 发起云端发现预览

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "存在最近一次 status=done 的引用扫描快照，快照内含多云可解析引用；台账当前为空；操作者具备 OpsEngineer 角色"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
      - entity_type: "CertReference"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "ScanSnapshot"
        field_constraints:
          - field: "product"
            value: "非 crd"
          - field: "cloud"
            value: "非空"
    state_requirements:
      - description: "台账（certificates 集合）为空，无任何证书记录"
        prerequisite_entity: "Certificate"
- Input: "运维人员以 OpsEngineer 身份请求发现预览端点 GET /api/v1/certs/discovery/preview"
- Output: "200 响应含唯一证书清单：按 cloud+accountKey+cloudCertId 三元组去重、排除 crd 引用与空 cloud 条目；每条目含七类字段与快照时点；预览为纯 DB 聚合，全程不发起任何云 API 调用"
- State: "无状态变更（只读聚合）"
- Side-effect: "none"

## Outcome "no-snapshot"
- Preconditions: "系统不存在任何 status=done 的引用扫描快照（可能存在 running 或 failed 快照，但无 done）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "不存在任何 status=done 的 ScanSnapshot"
        prerequisite_entity: "ScanSnapshot"
- Input: "运维人员以 OpsEngineer 身份请求发现预览端点 GET /api/v1/certs/discovery/preview"
- Output: "409 冲突响应，错误码为结构化 NO_SNAPSHOT（非 500），消息为固定安全文案引导先执行扫描；前端据此进入先执行扫描引导流程"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效登录会话（未认证）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "请求方未认证，认证判定先于角色判定与业务逻辑"
        prerequisite_entity: "CloudAccount"
- Input: "不带有效凭证请求 GET /api/v1/certs/discovery/preview"
- Output: "401 语义的认证失败响应，不进入任何业务逻辑，响应不含任何业务数据或端点内部细节"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "internal-repo-error"
<!-- source: inferred -->
<!-- reasoning: Fact Table DISCOVERY_INLEDGER_DUAL_CHANNEL（discovery_preview_service.go:190-227）：映射仓储读取故障时 buildEntry 显式返回错误（预览单一只读目的，仓储故障应显式 500 而非降级误判可导入）；WriteError 将未识别错误统一映射 500 INTERNAL_ERROR（response.go:97-103） -->
- Preconditions: "存在 done 快照，但 CloudCertMapping 仓储读取发生故障（注入式仓储错误）"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
      - entity_type: "CertReference"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "ScanSnapshot"
    state_requirements:
      - description: "CloudCertMapping 仓储读取注入故障"
        prerequisite_entity: "CloudCertMapping"
- Input: "运维人员以 OpsEngineer 身份请求发现预览端点"
- Output: "500 响应，错误码 INTERNAL_ERROR，消息为固定安全文案，不泄漏仓储故障细节与堆栈"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 预览端点始终纯 DB 聚合（快照引用 + 台账指纹/映射比对），绝不调用云 API，响应小于 1 秒
- 全部发现端点（预览/导入/进度）对非 OpsEngineer 角色一律 403，未认证 401 先于角色判定

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ScanSnapshot"        # done 快照（success / internal-repo-error）
      min_count: 1
      field_constraints:
        - field: "status"
          value: "done"
    - entity_type: "CertReference"       # 非空 cloud、非 crd 的快照引用
      min_count: 2
      relationship_type: "belongs_to"
      parent_entity: "ScanSnapshot"
    - entity_type: "CloudAccount"        # 系统可用云账号（no-snapshot / unauthorized 分支最小实体）
      min_count: 1
  state_requirements:
    - description: "success 分支台账为空；no-snapshot 分支无 done 快照；unauthorized 分支请求未认证"
      prerequisite_entity: "Certificate"
```

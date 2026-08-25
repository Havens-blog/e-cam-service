---
journey: "first-ledger-import"
step: 4
step-action: "会话逐条处理导入条目"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/first-ledger-import/journey.md
skip_eval: true
---

# Contract: first-ledger-import / Step 4: 会话逐条处理导入条目

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "会话 running 且含一条可解析条目：对应云适配器已注册、云账号 active 存在、云侧证书存在且返回可净化 PEM"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
          - field: "items"
            value: "至少一条 pending 可解析条目"
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "name"
            value: "与条目 accountKey 一致"
    state_requirements:
      - description: "云侧证书存在且 GetCert 返回仅 CERTIFICATE 块的可净化材料（叶在前 fullchain）"
        prerequisite_entity: "CloudAccount"
- Input: "会话后台逐条执行：GetCert 取公钥 PEM → 仅 CERTIFICATE 块净化 → 解析 → 指纹登记（fingerprint_only）→ CloudCertMapping 幂等建档"
- Output: "条目记 success 且 mappedCertId 有值；解析成功；成功条目触发占位指纹引用回填"
- State: "台账新增 fingerprint_only 记录（CertPEM 仅含 CERTIFICATE 块、无私钥材料、hostingStatus=fingerprint_only）；按三元组建立云证书映射；progress.succeeded 递增"
- Side-effect: "实时调用云 GetCert API（逐证限速）；占位指纹引用批量回填写"

## Outcome "item-failure-continues"
- Preconditions: "会话含至少两条条目：某条目在导入过程失败（如解析失败），其后仍有可成功条目"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
          - field: "items"
            value: "至少两条条目，其一注定失败"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "第一条目导入路径注定失败（如 PEM 无法解析），后续条目可正常成功"
        prerequisite_entity: "CloudAccount"
- Input: "运维人员观察会话继续处理后续条目（无用户动作，后台执行）"
- Output: "失败条目记录 errorReason（错误码+静态文案，不携带云响应片段）；会话不中断，其余条目正常收敛"
- State: "失败条目无台账/映射写入；progress.failed 递增；后续成功条目正常登记"
- Side-effect: "none"

## Outcome "cert-deleted-after-preview"
- Preconditions: "预览生成后、导入执行前，云侧该证书已被删除（预览与导入间状态漂移）"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
          - field: "items"
            value: "至少一条可解析 pending 条目"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "导入时点云侧该 cloudCertId 已不存在（GetCert Exists=false）"
        prerequisite_entity: "CloudAccount"
- Input: "会话处理该条目时实时 GetCert 校验"
- Output: "GetCert 判定云侧不存在，该条记因云侧已不存在而失败跳过，不阻塞其余条目"
- State: "该条目无任何台账/映射写入"
- Side-effect: "none"

## Journey Invariants
- 私钥全程不落库不入日志：入库 CertPEM 含且仅含 CERTIFICATE 块，不含私钥字样；fingerprint_only 形态
- 单条失败或 panic 不中断导入会话
- 云凭证仅内存使用禁入日志

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "DiscoveryImportSession"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "running"
    - entity_type: "CloudAccount"
      min_count: 1
  state_requirements:
    - description: "success 分支云侧证书存在可净化；failure 分支首条注定失败；deleted 分支云侧已删除"
      prerequisite_entity: "CloudAccount"
```

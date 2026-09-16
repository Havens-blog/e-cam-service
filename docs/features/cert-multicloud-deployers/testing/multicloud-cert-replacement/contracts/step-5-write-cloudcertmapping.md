---
journey: "multicloud-cert-replacement"
step: 5
step-action: "云证书 ID 写入 CloudCertMapping"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/multicloud-cert-replacement/journey.md
skip_eval: true
---

# Contract: multicloud-cert-replacement / Step 5: 云证书 ID 写入 CloudCertMapping

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "上传段已返回云证书 ID（各云归一形态），条目处于两段式中间点"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running（上传成功后、绑定段前）"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "同 (证书指纹, 云, 账号) 键无既有映射或可被幂等覆盖"
        prerequisite_entity: "CloudCertMapping"
- Input: "系统将 UploadCert 产生的云证书 ID 按云归一形态写入 CloudCertMapping"
- Output: "映射记录落库且状态为 active；ID 形态按云区分（华为 SCM UUID / AWS ACM ARN / Azure KV 版本化 secret ID 引用），三云 ID 空间互斥不混淆"
- State: "映射先行写入（先于绑定段，两段式去重与崩溃恢复锚点）：按 (certFingerprint, cloud, accountKey) 唯一键 Upsert，uploadedAt 默认当前时间；随后才允许进入绑定段"
- Side-effect: "none"

## Outcome "abnormal-id-explicit-rejection"
<!-- 事实对齐：映射 Upsert 本身无 ID 形态校验（repository/cloud_cert_mapping.go:28-43）；显式拒绝发生在绑定/回读路径——CloudFront 收到非 ACM ARN 的 IAM-hosted ID 显式拒绝（aws/cert.go:247-252）、监听器 ARN 与证书 ARN 地域不一致拒绝（:342-346）、空 ID 在清理/回读路径 ErrInvalidTarget（cloud_api_channel.go:283-285）。 -->
- Preconditions: "上传/引用返回的证书 ID 异常形态（如 CloudFront 目标收到非 ACM ARN 的 IAM-hosted ID、监听器 ARN 与证书 ARN 地域不一致、或 ID 为空）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
    state_requirements:
      - description: "云证书 ID 形态不符合目标云/产品的预期（注入式异常 ID）"
        prerequisite_entity: "ChangeItem"
- Input: "系统以异常形态 ID 继续绑定/回读链路"
- Output: "显式失败不猜测：绑定段显式拒绝（非 ARN 拒绝文案 / 地域不一致文案 / 空 ID 无效目标错误），变更条目失败并记 EXEC_FAILED 静态文案，不进入验证窗口"
- State: "不产生以异常 ID 为绑定的成功终态；已上传证书经补偿链路转 orphan 入清理队列"
- Side-effect: "补偿 CleanupOrphan 云调用（best-effort）"

## Outcome "upsert-idempotent-overwrite"
<!-- source: inferred -->
<!-- reasoning: Fact Table MC_MAPPING_UK（repository/indexes.go:75-78 唯一索引 uk_fp_cloud_account）+ Upsert 同键 $set 覆盖语义（repository/cloud_cert_mapping.go:28-43）：重跑/重复执行按同键覆盖，不产生第二行 -->
- Preconditions: "同 (certFingerprint, cloud, accountKey) 键已存在映射记录（如崩溃恢复后的重跑）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active（旧执行遗留）"
      - entity_type: "ChangeItem"
        min_count: 1
- Input: "系统对同键再次 Upsert 新云证书 ID"
- Output: "同键幂等覆盖（不新增行），记录以最新执行为准且状态 active"
- State: "CloudCertMapping 行数不变，cloudCertId 与 uploadedAt 更新"
- Side-effect: "none"

## Journey Invariants
- 云证书 ID 空间互斥（SCM ID / ACM ARN / KV 引用），映射唯一性天然成立，禁止跨云混用或猜测归一
- 两段式顺序不可逆：UploadCert 成功前不得 BindResource
- 映射唯一键 (certFingerprint, cloud, accountKey) 下任意时刻同键至多一行 active/orphan 记录

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeItem"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "running"
    - entity_type: "CloudCertMapping"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "success: 可为空态；upsert 分支: 既有 active 同键记录"
    - entity_type: "CloudAccount"
      min_count: 1
```

---
journey: "unsupported-entries-skip"
step: 3
step-action: "不可解析条目逐条记因跳过"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/unsupported-entries-skip/journey.md
skip_eval: true
---

# Contract: unsupported-entries-skip / Step 3: 不可解析条目逐条记因跳过

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "unsupported-cloud-skipped"
- Preconditions: "会话队列中含华为云条目（无 PEM 能力），无论其经前端灰选防护绕过还是直接构造请求提交（服务端同口径处理）；其后仍有可解析条目"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
          - field: "items"
            value: "含 huawei 条目与后续正常条目"
- Input: "会话处理不可解析条目（华为云无 PEM 能力）"
- Output: "该条目记因跳过：失败因为该云暂不支持自动解析语义的静态文案（错误码+固定文案），不产生任何台账/映射写入，也不阻塞后续可解析条目的正常导入"
- State: "无台账/映射/引用回填写入；progress.failed 递增；后续条目正常处理"
- Side-effect: "支持性预检不发起云 API 调用"

## Outcome "iam-hosted-skipped"
- Preconditions: "会话队列中含 AWS IAM-hosted（非 ARN 形态）证书 ID 条目，GetCert 对该形态显式报错不支持"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
          - field: "items"
            value: "含 aws 非 ARN 形态条目"
- Input: "会话处理该条目"
- Output: "记因暂不支持语义的静态失败因（与华为云同组语义），不阻塞其余条目；预览侧该条目本应 parseable=false 不可选（两侧口径一致）"
- State: "无任何写入"
- Side-effect: "none"

## Outcome "cert-gone-skipped"
- Preconditions: "预览时条目可解析且可选，确认导入前云侧证书已被删除（状态漂移）"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
          - field: "items"
            value: "含可解析 pending 条目"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "导入时点云侧该证书已删除（GetCert Exists=false）"
        prerequisite_entity: "CloudAccount"
- Input: "会话处理该条目时实时 GetCert 校验"
- Output: "GetCert 判定不存在，记因云侧已不存在语义的静态失败因跳过，不阻塞其余条目；预览明确标注基于快照时点以界定责任"
- State: "无任何写入"
- Side-effect: "实时 GetCert 云 API 调用（校验存在性）"

## Journey Invariants
- 不可解析/不可选条目绝不产生台账写入、映射建档或引用回填
- 任何单条跳过不阻塞会话中其余条目的处理
- 导入时逐条实时 GetCert 校验，以导入时点云侧状态为准

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
    - description: "三个分支分别注入 huawei 条目 / aws 非 ARN 条目 / 云侧已删除的可解析条目"
      prerequisite_entity: "DiscoveryImportSession"
```

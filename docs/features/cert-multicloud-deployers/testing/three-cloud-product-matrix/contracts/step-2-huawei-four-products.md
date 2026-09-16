---
journey: "three-cloud-product-matrix"
step: 2
step-action: "华为云四产品执行两段式"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/three-cloud-product-matrix/journey.md
skip_eval: true
---

# Contract: three-cloud-product-matrix / Step 2: 华为云四产品执行两段式

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "huawei-four-products-success"
- Preconditions: "华为云 cdn/waf/alb/nlb 四组合引用就绪；SCM 证书库可达；华为凭据有效"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 4
        field_constraints:
          - field: "product"
            value: "华为 cdn/waf/alb/nlb 各一，status=pending"
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "huawei"
- Input: "运维人员对华为云 cdn/waf/alb/nlb 四组合执行上传与绑定两段式"
- Output: "证书上传经 SCM 导入（统一以 CDN 口径上传、关闭重复校验）返回 SCM 证书 ID（UUID 形态）；cdn 走域名多证书更新（HTTPS 开启、SCM 托管类型）、waf 走主机定位后更新、alb/nlb 走监听器跨域定位后更新（默认证书引用切换、SNI 列表保留）；SCM ID 形态写入 CloudCertMapping"
- State: "四组合条目 success；四条 active 映射（SCM UUID 形态）"
- Side-effect: "SCM/WAF/ELB 云 API 调用（每云 20 QPS 限流）"

## Outcome "bind-resolve-failure-isolated"
- Preconditions: "华为某组合绑定段资源解析失败（如 WAF 主机名无精确匹配实例、ELB 监听器协议非 HTTPS/TERMINATED_HTTPS/TLS、或云证书 ID 为空）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "product"
            value: "华为某组合（资源解析失败）"
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "product"
            value: "其他组合（正常执行）"
    state_requirements:
      - description: "目标资源无法定位或协议不满足（注入式解析失败）"
        prerequisite_entity: "CloudAccount"
- Input: "系统尝试以异常 ID/不可定位资源执行绑定"
- Output: "显式失败不猜测：该组合条目失败记 EXEC_FAILED 静态文案，不写脏映射（映射随补偿转 orphan）；其余组合隔离不受影响"
- State: "该组合条目 failed；其余组合继续执行"
- Side-effect: "补偿 CleanupOrphan 云调用"

## Outcome "upload-name-conflict-immediate-retry"
<!-- source: inferred -->
<!-- reasoning: Fact Table MC_UPLOAD_NAME（deployer_common.go:72-79 名称单点公式）+ isCertNameConflictErr 即时重试无退避（deployer_common.go:31-63）+ 每次尝试新名称（huawei_deployer.go:142-164，C7 重试即新副本） -->
- Preconditions: "上传段生成的证书名与云侧既有证书名冲突"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "product"
            value: "华为任一组合"
    state_requirements:
      - description: "SCM 侧存在同名证书（注入式名称冲突）"
        prerequisite_entity: "CloudAccount"
- Input: "系统执行 SCM 上传"
- Output: "名称冲突按即时重试语义处理（无退避等待），重试立即换新名称副本；耗尽后显式失败不猜测"
- State: "重试成功则条目继续两段式；耗尽则 failed"
- Side-effect: "云上传重试调用"

## Journey Invariants
- 9 个 cloud×product 组合共用同一五方法端口语义与两段式编排，云差异只在每云适配层归一
- 单组合失败隔离：任一组合的失败/跳过不影响其余组合的执行与终态
- 云证书 ID 形态按云固定：华为 SCM ID；形态校验失败显式报错不猜测

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeItem"
      min_count: 4
      field_constraints:
        - field: "product"
          value: "华为 cdn/waf/alb/nlb"
    - entity_type: "CloudAccount"
      min_count: 1
      field_constraints:
        - field: "provider"
          value: "huawei"
```

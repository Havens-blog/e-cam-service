---
journey: "multicloud-cert-replacement"
step: 3
step-action: "上传段——证书上传至各云证书库"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/multicloud-cert-replacement/journey.md
skip_eval: true
---

# Contract: multicloud-cert-replacement / Step 3: 上传段——证书上传至各云证书库

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "条目已被认领执行；台账旧证书 hostingStatus=complete（信封加密私钥可解密取出）；三云凭据可用且与部署器云域一致；云证书库可达"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running（两段式上传段）"
      - entity_type: "Certificate"
        min_count: 2
        field_constraints:
          - field: "hostingStatus"
            value: "旧证书 complete"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "系统代表运维执行两段式上传段：UploadCert 将新证书（证书链 + 明文私钥）上传至目标云证书库（华为 SCM ImportCertificate / AWS ACM ImportCertificate / Azure KV 证书导入 REST）"
- Output: "各云返回云证书 ID：华为 SCM 证书 ID（UUID 形态、全局无地域）、AWS ACM 证书 ARN（CloudFront 目标固定 us-east-1 地域上传）、Azure KV 版本化 secret ID 引用；上传名称由单点公式生成且每次尝试（含重试）使用新名称"
- State: "云证书库新增证书；云证书 ID 进入执行上下文；上传成功后映射先行写入 active（崩溃恢复锚点，详见 Step 5）"
- Side-effect: "云上传 API 调用（经每云 20 QPS 限流器与有界退避）；私钥明文仅内存传递、通道与适配层用后归零，不落日志"

## Outcome "upload-rate-limited-retry"
<!-- source: inferred -->
<!-- reasoning: Fact Table MC_RATE_LIMITED_ITEM（execute_service.go:682-691：ErrCloudRateLimited → 条目 rate_limited）+ 有界重试核心（deployer_common.go:31-63，默认 5 次、1s/2s/4s/8s 退避、15s 总等待封顶）+ 每次重试新上传名称（huawei_deployer.go:142-164 等，C7 重试即新副本） -->
- Preconditions: "上传段云 API 返回限流（ErrCloudRateLimited 哨兵，如 KV 请求计数 429）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
    state_requirements:
      - description: "云侧对该凭据触发限流（注入式限流响应）"
        prerequisite_entity: "CloudAccount"
- Input: "系统执行上传段并遇云侧限流"
- Output: "条目标记 rate_limited 并按有界退避自动重试（退避耗尽或总等待封顶后转 failed 记静态失败因）；每次重试生成新上传名称不复用旧名称"
- State: "条目 rate_limited →（重试成功）success 或（耗尽）failed；未成功上传前不写映射"
- Side-effect: "云上传 API 限流重试调用（有界）"

## Outcome "crash-before-bind-recovery"
- Preconditions: "UploadCert 已返回云证书 ID 且映射已先行落库（active 锚点），进程在 BindResource 开始前中断（心跳停更超时阈值）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running 且心跳时间已超时"
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active（上传后绑定前）"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "运维人员重跑或等待心跳超时恢复链路收敛"
- Output: "心跳超时恢复将条目标记 failed(EXEC_TIMEOUT)（默认 30 分钟阈值，恢复任务周期巡检并告警）；重跑重新走两段式上传新副本，不复用悬空 ID 直接绑定"
- State: "条目 failed(EXEC_TIMEOUT)；重跑的新执行按映射唯一键覆盖旧记录并产生新云证书 ID"
- Side-effect: "none"

## Journey Invariants
- 两段式顺序不可逆：UploadCert 成功前不得 BindResource；绑定失败必经 CleanupOrphan 补偿，不允许跳过补偿直接重试
- 云证书 ID 空间互斥（SCM ID / ACM ARN / KV 引用）
- 私钥明文仅内存传递、用后 Zeroize；云端错误细节不进 API 响应仅入日志

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeItem"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "success/rate-limited: running；crash 分支: running 且心跳超时"
    - entity_type: "Certificate"
      min_count: 2
      field_constraints:
        - field: "hostingStatus"
          value: "旧证书 complete"
    - entity_type: "CloudCertMapping"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "crash 分支为 active（上传后绑定前）"
    - entity_type: "CloudAccount"
      min_count: 1
```

---
journey: "multicloud-cert-replacement"
step: 1
step-action: "为三云证书引用生成变更清单"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/multicloud-cert-replacement/journey.md
skip_eval: true
---

# Contract: multicloud-cert-replacement / Step 1: 为三云证书引用生成变更清单

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "操作者具备 ops_engineer 角色的有效会话；台账存在待替换旧证书（hostingStatus=complete）与就绪新证书；引用扫描已产出含三云（华为/AWS/Azure）产品引用的 done 快照；无在途变更单占用旧证书指纹"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 2
        field_constraints:
          - field: "hostingStatus"
            value: "旧证书为 complete（私钥材料在库）"
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
      - entity_type: "CertReference"
        min_count: 1
        field_constraints:
          - field: "cloud"
            value: "华为/AWS/Azure 任一，产品为该云已注册组合"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "无 active 变更单占用旧证书指纹（互斥令牌空闲）"
        prerequisite_entity: "ChangeOrder"
- Input: "POST /api/v1/certs/changes 发起变更清单生成（旧证书指纹 + 新证书 ID）"
- Output: "201 变更清单 VO：三云引用条目 AutoChangeable=true（云通道判定恒可执行），与 aliyun/tencent 引用同栏呈现、计入执行成功率分母；无 ERR_DISCOVERY_ONLY 分区、无 skipped 标记"
- State: "新建变更单 Status=pending_confirm 且 ActiveMutex=旧证书指纹；三云条目以 pending 状态入清单"
- Side-effect: "none"
- Invariants: "云通道可执行性判定无云差异（assessChangeable 对 cloud_api 通道无条件返回可执行）"

## Outcome "k8s-managed-nonexecutable"
- Preconditions: "清单中某引用的目标资源为 K8s 通道且命中不可执行判定（探得 GitOps 标签/ownerReferences/托管注解任一管理信号、或管理探测缺失、或探测失败）；同清单其余为三云云通道引用"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 2
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
      - entity_type: "CertReference"
        min_count: 2
        field_constraints:
          - field: "channel"
            value: "混合：至少一条 k8s_api 通道托管资源引用 + 至少一条三云云通道引用"
- Input: "发起包含 K8s 托管资源引用在内的变更清单生成"
- Output: "K8s 托管引用条目 Status=skipped 且 Error 记 K8S_MANAGEMENT_* 原因前缀（管理信号/未探测/探测失败），不入执行成功率分母，引导走控制器/CRD 更新；同清单三云引用 AutoChangeable=true 正常可执行"
- State: "变更单仍创建；skipped 条目与原因一并持久化；三云条目 pending"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效会话（全局认证中间件无法从会话取得身份）"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
    state_requirements:
      - description: "无有效会话（认证在上游全局中间件被拒，不进入角色判定）"
        prerequisite_entity: "Certificate"
- Input: "POST /api/v1/certs/changes 无有效会话调用"
- Output: "HTTP 401，响应体为全局认证中间件的认证失败文案（非 cert 模块 Envelope 包装），不泄露敏感数据"
- State: "无状态变化，不创建变更单"
- Side-effect: "none"

## Journey Invariants
- 三云替换语义与 aliyun/tencent 完全同构：失败状态、回滚、清理走同一状态机，不引入云特定的新验证/回滚机制
- 两段式顺序不可逆：UploadCert 成功前不得 BindResource；绑定失败必经 CleanupOrphan 补偿，不允许跳过补偿直接重试
- 云证书 ID 空间互斥（SCM ID / ACM ARN / KV 引用），映射唯一性天然成立，禁止跨云混用或猜测归一
- 私钥明文仅内存传递、用后 Zeroize；云端错误细节不进 API 响应仅入日志
- 变更清单可执行性唯一受 K8s 管理权/托管资源约束，三云不因云差异降级为 discovery-only

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "Certificate"
      min_count: 2
      field_constraints:
        - field: "hostingStatus"
          value: "success 分支旧证书 complete；unauthorized 分支仅需存在即可"
    - entity_type: "ScanSnapshot"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "done"
    - entity_type: "CertReference"
      min_count: 2
      field_constraints:
        - field: "cloud"
          value: "覆盖三云云通道引用；k8s 分支另含托管资源引用"
    - entity_type: "CloudAccount"
      min_count: 1
  state_requirements:
    - description: "success 分支互斥令牌空闲；unauthorized 分支无有效会话"
      prerequisite_entity: "ChangeOrder"
```

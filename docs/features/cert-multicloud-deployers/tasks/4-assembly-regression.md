---
id: "4"
title: "模块装配：三云 RegisterDeployer + discoveryOnlyClouds 移除 + 清单可执行回归"
priority: "P0"
estimated_time: "1.5h"
complexity: "medium"
dependencies: [1,2,3]
surface-key: ""
surface-type: "api"
breaking: true
type: "coding.feature"
mainSession: false
---

# 4: 模块装配：三云 RegisterDeployer + discoveryOnlyClouds 移除 + 清单可执行回归

## Description

将 Task 1-3 的三云部署器装配进运行时：`module.go` `RegisterDeployer`（产品集与扫描面一致）、扫描适配改用完整 CertAdapter（与部署器共享实例，aliyun 模式）、`discoveryOnlyClouds` 移除 huawei/aws/azure——使三云引用在变更清单中成为可执行项（`AutoChangeable=true`），不再 `ERR_DISCOVERY_ONLY` 分区。含清单生成回归测试。

## Reference Files
- `docs/proposals/cert-multicloud-deployers/proposal.md` — In Scope / Success Criteria / Key Risks
- `internal/cert/module.go` — RegisterDeployer 装配点（aliyun/tencent 现有模式）+ NewXxxScanAdapter 装配
- `internal/cert/service/changelist_generator.go` — discoveryOnlyClouds 名单与 assessChangeable 判定
- `internal/cert/service/changelist_generator_test.go` — 清单可执行性回归测试注入点

## Acceptance Criteria

- [ ] `module.go` 对 huawei/aws/azure 三云 `RegisterDeployer`（产品集：华为 cdn/waf/alb/nlb、AWS cdn/alb/nlb、Azure cdn/alb），并 `Stop()` 透传
- [ ] 扫描适配 `NewHuaweiScanAdapter`/`NewAwsScanAdapter`/`NewAzureScanAdapter` 改用完整 CertAdapter 实例（与部署器共享，aliyun 模式），替换 discovery-only 适配器装配
- [ ] `discoveryOnlyClouds` 移除 huawei/aws/azure；`assessChangeable` 对三云云通道返回 true（可执行）
- [ ] 回归测试：三云产品引用生成变更清单时 `AutoChangeable=true`、不再含 `ERR_DISCOVERY_ONLY` 原因；discovery-only 空名单后无引用落入不可执行分区
- [ ] 全仓 `go build ./...` 通过 + cert 域测试套件（deployer/service/web）全绿

## Hard Rules

- 仅修改：`internal/cert/module.go`、`internal/cert/service/changelist_generator.go`（及测试）、三云 cloudx 适配装配点；不触碰各云部署器实现本体（Task 1-3 产物）
- `discoveryOnlyClouds` 移除后不得遗留对三云的不可执行静态判定（唯一不可执行来源回归为 K8s 管理权/托管资源）
- 装配不改变既有 aliyun/tencent 行为（回归锁定）

## Implementation Notes

### Test Impact
- Affected test suite(s): `internal/cert/service/changelist_generator_test.go`（新增三云可执行回归）、`internal/cert/web/`（若装配影响 handler 构造）
- Expected fixture changes: 无既有 fixture 破坏（discoveryOnlyClouds 移除只会把「原本 skipped 的项」变为可执行——若有测试断言三云 skipped 需同步）
- Risk level: medium

- 参照 module.go 中 aliyun/tencent 的 RegisterDeployer 现有写法（含 retry 策略、mappings 注入）；三云部署器 Stop 透传导配层限流器
- 清单生成回归测试建议复用 `changelist_generator_test.go` 的 `cloudRef`/`newGenHarness` 夹具，按三云各造一条云引用断言 AutoChangeable=true
- 若 discovery-only 适配器被完整 CertAdapter 取代后无引用，清理旧装配点（不静默留死代码）

---
id: "3"
title: "火山引用扫描适配器（4 产品 → CertReference）"
priority: "P0"
estimated_time: "2h"
complexity: "medium"
dependencies: []
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 3: 火山引用扫描适配器（4 产品 → CertReference）

## Description

把火山 CDN/WAF/ALB/NLB 资源的证书引用接入 `reference_scan_service`，使火山引用进入变更清单可执行项。适配器形态对齐 5 云 `NewXxxScanAdapter`（`service/discovery_adapter_*.go` 或 scan adapter 先例）：按账号枚举产品资源→解析绑定证书指纹→产出 `CertReference`（CDN/WAF 域名粒度、ALB/NLB served domains 展开）。火山证书库适配器（cert-volcano-import-sync 交付的 `cloudx/volcano/cert.go`）供指纹解析复用。

## Reference Files

- `docs/proposals/cert-volcano-deployer/proposal.md` — Proposed Solution / In Scope / Success Criteria
- `internal/cert/service/discovery_adapter.go` — 5 云扫描适配器包装形态（NewXxxScanAdapter）
- `internal/cert/service/reference_scan_service.go` — 引用扫描流程与 CertReference 写入（指纹解析/占位口径）
- `internal/shared/cloudx/volcano/cert.go` — 火山证书库适配器（GetCert/指纹基础）

## Acceptance Criteria

- [ ] `NewVolcanoScanAdapter` 对齐 5 云形态（`domain.Cloud("volcano")` 映射 + 编译期接口断言）
- [ ] CDN/WAF 域名粒度 + ALB/NLB served domains 展开的 `CertReference` 产出正确（指纹解析：映射反查→GetCert 要素→`certscan-unresolved:` 占位语义与 5 云一致）
- [ ] 单产品单账号失败隔离（不中断该云其余产品/账号）
- [ ] fake SDK 单测：4 产品引用枚举 + 指纹解析命中/占位两态 + 失败隔离

## Hard Rules

- 仅修改以下文件：`internal/cert/service/` 内新增火山扫描适配器文件 + 对应测试。
- 引用扫描只读：不调用任何云写方法。
- 不复制部署器逻辑：指纹解析复用 `cloudx/volcano/cert.go` 与既有映射反查语义。

## Implementation Notes

### Key Risks 缓解

- 引用粒度与绑定层对齐（M/M）：本任务产出的 resourceId 形态必须与任务 2 BindResource 消费的 resourceId 一致（CDN=域名、ALB/NLB=监听复合 ID→served domains）——两任务共享同一形态约定，交付时互相核对。

### File Scope

新增：`internal/cert/service/volcano_scan_adapter.go`（或对齐现有命名）+ `volcano_scan_adapter_test.go`。

### Test Impact

- 新文件 + 新测试；既有 5 云引用扫描测试全绿回归。风险 low。
---
id: "2"
title: "火山发现适配器包装与模块装配"
priority: "P0"
estimated_time: "1h"
complexity: "low"
dependencies: ["1"]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 2: 火山发现适配器包装与模块装配

## Description

把任务 1 的火山证书库适配器经 `service.NewVolcanoDiscoveryCertAdapter` 包装为 `DiscoveryCertAdapter`（对齐既有 aliyun/tencent/huawei/aws/azure 五云包装），并装配进 `internal/cert/module.go` 的发现导入适配器列表——使火山与现有五云同管线（幂等台账、映射补建、ALREADY_IN_LEDGER 全复用）。

## Reference Files

- `docs/proposals/cert-volcano-import-sync/proposal.md` — 装配（In Scope / Success Criteria）
- `internal/cert/service/discovery_adapter.go` — DiscoveryCertAdapter 接口与各云 NewXxxDiscoveryCertAdapter 包装模式
- `internal/cert/module.go` — 发现导入适配器列表（220-225 行）与扫描适配器装配对照

## Acceptance Criteria

- [ ] `NewVolcanoDiscoveryCertAdapter` 返回 `DiscoveryCertAdapter` 实现（编译期接口断言）
- [ ] `module.go` 发现导入适配器列表加入火山项（与现有五云并列，共享 CertAdapter/凭证装配）
- [ ] 既有五云发现导入单测与手工导入全链路测试全绿（新增装配不破坏现有行为）

## Hard Rules

- 仅修改以下文件：`internal/cert/service/discovery_adapter_volcano.go`（或对齐现有文件命名）、`internal/cert/module.go`、对应测试文件。不触碰其他域。
- 不复制适配器逻辑：包装层只做 DiscoveryCertAdapter 端口适配，云侧 List/Get 逻辑全部在任务 1 的 cloudx 适配器内。

## Implementation Notes

### 装配对齐

- 参照 `service.NewAliyunDiscoveryCertAdapter(aliyuncert.NewCertAdapter(logger))` 同构：`service.NewVolcanoDiscoveryCertAdapter(volcano.NewCertAdapter(logger))`。
- logger 传递与现有五云一致；signal 处对齐 `InitCertModule` 参数（deps.account 现成）。

### Test Impact

- 新增包装层单测（fake cloudx 适配器）；module 装配保持现有测试全绿。风险低。
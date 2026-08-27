---
id: "1"
title: "新增共享默认资产类型常量（含 dns）并接入 AutoSyncScheduler"
priority: "P0"
estimated_time: "1h"
complexity: "medium"
dependencies: []
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.fix"
mainSession: false
---

# 1: 新增共享默认资产类型常量（含 dns）并接入 AutoSyncScheduler

## Description

`AutoSyncScheduler.triggerSync` 在账号 `SupportedAssetTypes` 为空时使用的默认资产类型清单遗漏了 `dns`，导致默认配置的账号 DNS 域名与解析记录永不自动同步（静默跳过，无日志）。本任务定义唯一的默认同步资产类型常量（现有清单 + `dns`），替换调度器硬编码，并用单元测试锁定默认与显式配置两种语义。

## Reference Files
- `docs/proposals/dns-autosync-fix/proposal.md` - Problem、Proposed Solution、Scope、Key Risks
- `internal/cam/scheduler/auto_sync.go`: triggerSync 默认 assetTypes 硬编码清单替换为共享常量（现 223-229 行）
- `internal/cam/domain/asset_types.go`（新建）: 常量定义位置，需被 scheduler 与 executor 双方引用且无 import cycle

## Acceptance Criteria
- [ ] 仓库存在唯一定义的默认同步资产类型清单常量（= 原清单 + `dns`，其他类型不变），scheduler 与 executor 均可引用，编译通过（无 import cycle）
- [ ] `auto_sync.go` triggerSync 默认 assetTypes 来自共享常量，文件内不再有本地硬编码清单
- [ ] 单元测试：`SupportedAssetTypes` 为空时，提交的任务 `params.asset_types` 包含 `"dns"`
- [ ] 单元测试：`SupportedAssetTypes` 显式配置时，任务参数与配置完全一致（配置不含 `dns` 时不强制加入）
- [ ] `go build ./...` 与 `internal/cam/scheduler` 包测试通过

## Hard Rules
- 默认清单除新增 `dns` 外不得增删任何其他类型（含不得顺手加 `eni`）

## Implementation Notes
- 常量放置建议 `internal/cam/domain`（asset_sync.go 已引用该包；scheduler 新增 import 不构成环）。若实际存在环，可放 `internal/shared/domain`，以编译通过为准。
- 风险：新增 `dns` 会增加每次自动同步的 ListRecords API 调用（每域名一次），DNS 记录量通常小，上线后观察任务耗时即可。
- 风险提示（proposal Key Risks）：显式配置不含 `dns` 的存量账号依旧不同步 DNS，这是预期语义，不要在本任务里"顺手修复"。

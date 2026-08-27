---
id: "2"
title: "收敛执行器与服务层硬编码资产清单到共享定义"
priority: "P1"
estimated_time: "1h"
complexity: "medium"
dependencies: ["1"]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.fix"
mainSession: false
---

# 2: 收敛执行器与服务层硬编码资产清单到共享定义

## Description

默认/网络资产类型清单在 `sync_assets.go`（expandAssetTypes 的 network 展开等）与 `asset_sync.go` 中独立硬编码，与调度器的清单各自漂移（本次 `dns` 遗漏即由此产生）。本任务把这些语义相同的清单收敛到任务 1 的共享定义，行为等价替换，并用测试锁定 `dns` 的透传与 network 展开行为。

## Reference Files
- `docs/proposals/dns-autosync-fix/proposal.md` - Proposed Solution、Scope、Key Risks
- `internal/cam/task/executor/sync_assets.go`: expandAssetTypes 的 network/database/storage/middleware/compute 展开清单收敛（现 265-318 行）
- `internal/cam/task/executor/expand_types_test.go`: 补充 `dns` 透传与 network 展开断言
- `internal/cam/service/asset_sync.go`: 201/266/560 行附近的资产类型清单收敛

## Acceptance Criteria
- [ ] `expandAssetTypes` 的 network 展开清单引用共享常量，内容不变（`vpc/vswitch/eip/eni/lb/cdn/waf/dns`）
- [ ] `asset_sync.go` 中语义相同的默认/网络清单引用共享定义，行为等价替换（不改变任何类型集合）
- [ ] grep 验证：`auto_sync.go`、`sync_assets.go`、`asset_sync.go` 三处不再有各自维护的重复默认资产清单硬编码
- [ ] 测试断言：`"dns"` 作为独立类型经 expandAssetTypes 原样透传；`"network"` 展开结果包含 `"dns"`
- [ ] `go build ./...`、`go vet ./internal/cam/...` 与 `internal/cam/task/executor`、`internal/cam/service` 包测试通过

## Hard Rules
- 行为等价替换：除清单来源统一外，不得改变任何展开结果或同步行为
- 仅修改以下文件：`internal/cam/task/executor/sync_assets.go`、`internal/cam/task/executor/expand_types_test.go`、`internal/cam/service/asset_sync.go`（以及任务 1 已建的共享常量文件的引用）

## Implementation Notes
- 展开清单（network -> 子类型）与默认清单是两种语义，可定义为两组共享常量；只有语义相同的才收敛，不要强行合并不同语义的清单。
- 注意 mock 完整性：历史上 executor 测试 mock 曾因接口方法缺失导致整包测试失败（见 split-sync-assets-executor 教训），跑测试前先确认 `mockCloudAdapter` 实现完整。

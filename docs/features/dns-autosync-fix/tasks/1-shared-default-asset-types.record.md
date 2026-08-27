# Task 1 Record: 共享默认资产类型常量（含 dns）

**Status**: completed
**Completed**: 2026-08-27
**Executed in**: main session（forge CLI 不可用，回退直执行）

## Changes

| File | Change |
|------|--------|
| `internal/shared/domain/asset_types.go` | 新建 `DefaultSyncAssetTypes` 常量 = 原调度器硬编码清单 + `dns`，其他类型不变 |
| `internal/cam/scheduler/auto_sync.go` | triggerSync 中硬编码默认清单移除，提取纯函数 `buildSyncParams(account)`：`SupportedAssetTypes` 为空时回退 `domain.DefaultSyncAssetTypes`，否则原样透传 |
| `internal/cam/scheduler/auto_sync_test.go` | 新建三个单元测试（见下） |

常量放在 `internal/shared/domain`（非建议首选 `internal/cam/domain`），原因：scheduler、executor、service 三方均已 import 该包，零新增依赖边，无 import cycle。

## Tests

- `TestBuildSyncParams_DefaultIncludesDNS` — 空配置 → `asset_types` 含 `"dns"`（PASS）
- `TestBuildSyncParams_ExplicitConfigPreserved` — 显式 `["ecs","rds"]` 原样保留，不强制注入 dns（PASS）
- `TestDefaultSyncAssetTypes_NoDuplicates` — 常量清单无重复项（PASS）

质量门禁：`go build ./...` ✅、`go vet`（scheduler + shared/domain）✅、`go test ./internal/cam/scheduler/ ./internal/shared/domain/` ✅（-count=1 强制重跑确认）

## Hard Rule Compliance

默认清单除新增 `dns` 外未增删任何类型（未加 `eni`）。显式配置不含 dns 的存量账号语义未动。

## Outcome

默认配置账号的自动同步任务现在会携带 `dns`，经 `expandAssetTypes` 透传后在账号级触发 `syncDNS`。运行时生效需重启 e-cam-service（当前进程仍是 8 月 19 日旧二进制）。

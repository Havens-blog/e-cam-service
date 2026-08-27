# Task 2 Record: 收敛执行器与服务层硬编码资产清单

**Status**: completed
**Completed**: 2026-08-27
**Executed in**: main session（forge CLI 不可用，回退直执行）

## Changes

| File | Change |
|------|--------|
| `internal/shared/domain/asset_types.go` | 新增聚合展开常量 `ComputeAssetTypes` / `DatabaseAssetTypes` / `NetworkAssetTypes` / `StorageAssetTypes` / `MiddlewareAssetTypes`；新增 `DefaultCMDBSyncAssetTypes`（CMDB 服务路径默认清单，集合原样） |
| `internal/cam/task/executor/sync_assets.go` | expandAssetTypes 五个聚合展开清单改引用共享常量（集合不变）；Execute() 中任务参数为空时的默认清单（与 `DefaultSyncAssetTypes` 同集合、仅顺序不同）替换为共享常量 |
| `internal/cam/task/executor/expand_types_test.go` | 新增 `TestExpandAssetTypes_DNSPassthrough`、`TestExpandAssetTypes_NetworkIncludesDNS` |
| `internal/cam/service/asset_sync.go` | SyncAssets/SyncAccountAssets 两处默认清单（文件内互为重复）收敛为 `DefaultCMDBSyncAssetTypes`；database/storage 聚合展开引用共享常量；network 聚合展开保留原集合并加漂移注释 |

## 行为等价性说明

- 所有替换均为集合恒等替换；唯一顺序差异：executor Execute() 默认清单原为 `vpc,eip,lb,vswitch,cdn...`，现随共享常量为 `vpc,vswitch,eip,lb,cdn...`——仅改变地域内同步顺序，结果集合与写入数据不变。
- asset_sync.go:553 的 network 聚合展开（无 vswitch）与 `NetworkAssetTypes` 历史已漂移，按 Hard Rule 不强行收敛，已加注释指向共享常量并说明待后续任务决策。
- asset_sync.go 默认清单与调度器默认清单是两种语义（含 eni、不含 vswitch/kafka/es），分别定义为 `DefaultCMDBSyncAssetTypes` 与 `DefaultSyncAssetTypes`，不合并。

## Tests

- 新增 2 个 executor 测试（dns 独立类型透传、network 展开含 dns）PASS
- `go build ./...` ✅、`go vet ./internal/cam/...` ✅
- `go test -count=1` executor / service / scheduler / shared/domain 四包全绿

## grep 验证（AC）

`auto_sync.go` / `sync_assets.go` / `asset_sync.go` 三文件中不再有各自维护的重复**默认**资产清单硬编码；唯一残留 `asset_sync.go:553` 为语义不同的聚合展开清单（见上）。

## Outcome

默认清单与聚合展开清单现全部收敛至 `internal/shared/domain/asset_types.go` 单一事实来源，dns 遗漏这类漂移今后只需改一处。

// Package sync 提供云资产同步服务
//
// 当前状态：待迁移
// 同步相关代码目前在 internal/cam/sync/ 和 internal/cam/scheduler/
//
// 迁移计划：
// 1. 将 cam/sync/domain/ 迁移到 sync/domain/
// 2. 将 cam/sync/service/ 迁移到 sync/service/
// 3. 将 cam/scheduler/ 迁移到 sync/scheduler/
// 4. 将 cam/service/asset_sync.go 迁移到 sync/service/
// 5. 将 cam/task/executor/sync_assets.go 迁移到 sync/executor/
package sync

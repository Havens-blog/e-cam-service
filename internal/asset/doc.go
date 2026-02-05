// Package asset 提供云资产管理功能
//
// 当前状态：待迁移
// 资产相关代码目前在 internal/cam/ 下的多个文件中
//
// 迁移计划：
// 1. 将 cam/domain/instance.go, model.go, field.go 等迁移到 asset/domain/
// 2. 将 cam/repository/instance.go, model.go 等迁移到 asset/repository/
// 3. 将 cam/service/instance.go, model.go, asset.go 等迁移到 asset/service/
// 4. 将 cam/web/asset_handler.go, instance_handler.go 等迁移到 asset/web/
package asset

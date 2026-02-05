// Package account 提供云账号管理功能
//
// 当前状态：待迁移
// 云账号相关代码目前在 internal/cam/repository/account.go 和 internal/cam/service/account.go
//
// 迁移计划：
// 1. 将 cam/domain/account.go 迁移到 account/domain/
// 2. 将 cam/repository/account.go 迁移到 account/repository/
// 3. 将 cam/service/account.go 迁移到 account/service/
// 4. 创建 account/web/handler.go 处理云账号 API
package account

// Package sync 同步服务模块
// 这是从 internal/cam/sync 重构后的独立模块
// 当前阶段：别名模式，重新导出 cam/sync 的实现
package sync

import (
	camsync "github.com/Havens-blog/e-cam-service/internal/cam/sync/domain"
	camsyncservice "github.com/Havens-blog/e-cam-service/internal/cam/sync/service"
)

// 重新导出 domain 类型
type (
	CloudAccount  = camsync.CloudAccount
	CloudProvider = camsync.CloudProvider
	CloudAdapter  = camsync.CloudAdapter
	Region        = camsync.Region
	ECSInstance   = camsync.ECSInstance
)

// 云厂商常量
const (
	ProviderAliyun = camsync.ProviderAliyun
	ProviderAWS    = camsync.ProviderAWS
	ProviderAzure  = camsync.ProviderAzure
	ProviderHuawei = camsync.ProviderHuawei
	ProvicerQcloud = camsync.ProvicerQcloud
)

// 重新导出 service 类型
type SyncService = camsyncservice.SyncService

// NewSyncService 创建同步服务
var NewSyncService = camsyncservice.NewSyncService

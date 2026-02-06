// Package sync 同步服务模块
// 这是从 internal/cam/sync 重构后的独立模块
package sync

import (
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/sync/domain"
	"github.com/Havens-blog/e-cam-service/internal/sync/service"
	"github.com/gotomicro/ego/core/elog"
)

// 重新导出 domain 类型
type (
	CloudProvider  = domain.CloudProvider
	CloudAdapter   = domain.CloudAdapter
	Region         = domain.Region
	ECSInstance    = domain.ECSInstance
	DataDisk       = domain.DataDisk
	SyncTask       = domain.SyncTask
	SyncTaskStatus = domain.SyncTaskStatus
	SyncResult     = domain.SyncResult
	SyncError      = domain.SyncError
)

// 云厂商常量
const (
	ProviderAliyun  = domain.ProviderAliyun
	ProviderAWS     = domain.ProviderAWS
	ProviderAzure   = domain.ProviderAzure
	ProviderHuawei  = domain.ProviderHuawei
	ProviderVolcano = domain.ProviderVolcano
	ProviderTencent = domain.ProviderTencent
)

// 任务状态常量
const (
	TaskStatusPending   = domain.TaskStatusPending
	TaskStatusRunning   = domain.TaskStatusRunning
	TaskStatusSuccess   = domain.TaskStatusSuccess
	TaskStatusFailed    = domain.TaskStatusFailed
	TaskStatusCancelled = domain.TaskStatusCancelled
)

// 重新导出错误
var (
	ErrInvalidAccountName   = domain.ErrInvalidAccountName
	ErrInvalidProvider      = domain.ErrInvalidProvider
	ErrInvalidAccessKey     = domain.ErrInvalidAccessKey
	ErrAccountNotFound      = domain.ErrAccountNotFound
	ErrAccountDisabled      = domain.ErrAccountDisabled
	ErrAccountExpired       = domain.ErrAccountExpired
	ErrSyncTaskNotFound     = domain.ErrSyncTaskNotFound
	ErrSyncTaskRunning      = domain.ErrSyncTaskRunning
	ErrSyncConfigNotFound   = domain.ErrSyncConfigNotFound
	ErrInvalidRegion        = domain.ErrInvalidRegion
	ErrInvalidResourceType  = domain.ErrInvalidResourceType
	ErrAdapterNotFound      = domain.ErrAdapterNotFound
	ErrCredentialInvalid    = domain.ErrCredentialInvalid
	ErrAPICallFailed        = domain.ErrAPICallFailed
	ErrDataConversionFailed = domain.ErrDataConversionFailed
)

// 重新导出 service 类型
type SyncService = service.SyncService

// NewSyncService 创建同步服务
func NewSyncService(adapterFactory *cloudx.AdapterFactory, logger *elog.Component) *SyncService {
	return service.NewSyncService(adapterFactory, logger)
}

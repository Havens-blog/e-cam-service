// Package account 云账号管理模块
// 这是从 internal/cam 重构后的独立模块
// 当前阶段：别名模式，重新导出 cam 的实现
package account

import (
	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	camrepo "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	camservice "github.com/Havens-blog/e-cam-service/internal/cam/service"
)

// 重新导出 domain 类型
type (
	CloudAccount              = camdomain.CloudAccount
	CloudAccountFilter        = camdomain.CloudAccountFilter
	CreateCloudAccountRequest = camdomain.CreateCloudAccountRequest
	UpdateCloudAccountRequest = camdomain.UpdateCloudAccountRequest
	ConnectionTestResult      = camdomain.ConnectionTestResult
	SyncAccountRequest        = camdomain.SyncAccountRequest
	SyncResult                = camdomain.SyncResult
)

// 重新导出 repository 类型
type CloudAccountRepository = camrepo.CloudAccountRepository

// 重新导出 service 类型
type CloudAccountService = camservice.CloudAccountService

// NewCloudAccountRepository 创建云账号仓储
var NewCloudAccountRepository = camrepo.NewCloudAccountRepository

// NewCloudAccountService 创建云账号服务
var NewCloudAccountService = camservice.NewCloudAccountService

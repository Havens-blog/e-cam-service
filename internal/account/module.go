// Package account 云账号管理模块
// 这是从 internal/cam 重构后的独立模块
package account

import (
	"github.com/Havens-blog/e-cam-service/internal/account/repository"
	"github.com/Havens-blog/e-cam-service/internal/account/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/account/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// 重新导出 domain 类型 (来自 shared/domain)
type (
	CloudAccount              = domain.CloudAccount
	CloudAccountFilter        = domain.CloudAccountFilter
	CloudAccountStatus        = domain.CloudAccountStatus
	CloudAccountConfig        = domain.CloudAccountConfig
	CloudProvider             = domain.CloudProvider
	Environment               = domain.Environment
	CreateCloudAccountRequest = domain.CreateCloudAccountRequest
	UpdateCloudAccountRequest = domain.UpdateCloudAccountRequest
	ConnectionTestResult      = domain.ConnectionTestResult
	SyncAccountRequest        = domain.SyncAccountRequest
	SyncResult                = domain.SyncResult
)

// 重新导出状态常量
const (
	CloudAccountStatusActive   = domain.CloudAccountStatusActive
	CloudAccountStatusDisabled = domain.CloudAccountStatusDisabled
	CloudAccountStatusError    = domain.CloudAccountStatusError
	CloudAccountStatusTesting  = domain.CloudAccountStatusTesting
)

// 重新导出云厂商常量
const (
	CloudProviderAliyun     = domain.CloudProviderAliyun
	CloudProviderAWS        = domain.CloudProviderAWS
	CloudProviderAzure      = domain.CloudProviderAzure
	CloudProviderTencent    = domain.CloudProviderTencent
	CloudProviderHuawei     = domain.CloudProviderHuawei
	CloudProviderVolcano    = domain.CloudProviderVolcano
	CloudProviderVolcengine = domain.CloudProviderVolcengine
)

// 重新导出环境常量
const (
	EnvironmentProduction  = domain.EnvironmentProduction
	EnvironmentStaging     = domain.EnvironmentStaging
	EnvironmentDevelopment = domain.EnvironmentDevelopment
)

// 重新导出 DAO 类型
type CloudAccountDAO = dao.CloudAccountDAO

// 重新导出 repository 类型
type CloudAccountRepository = repository.CloudAccountRepository

// 重新导出 service 类型
type CloudAccountService = service.CloudAccountService

// NewCloudAccountDAO 创建云账号DAO
var NewCloudAccountDAO = dao.NewCloudAccountDAO

// NewCloudAccountRepository 创建云账号仓储
var NewCloudAccountRepository = repository.NewCloudAccountRepository

// NewCloudAccountService 创建云账号服务
var NewCloudAccountService = service.NewCloudAccountService

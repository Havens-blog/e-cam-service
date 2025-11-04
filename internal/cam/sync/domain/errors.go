package domain

import "errors"

var (
	// 账号相关错误
	ErrInvalidAccountName = errors.New("无效的账号名称")
	ErrInvalidProvider    = errors.New("无效的云厂商")
	ErrInvalidAccessKey   = errors.New("无效的访问密钥")
	ErrAccountNotFound    = errors.New("账号不存在")
	ErrAccountDisabled    = errors.New("账号已禁用")
	ErrAccountExpired     = errors.New("账号已过期")

	// 同步相关错误
	ErrSyncTaskNotFound   = errors.New("同步任务不存在")
	ErrSyncTaskRunning    = errors.New("同步任务正在运行")
	ErrSyncConfigNotFound = errors.New("同步配置不存在")
	ErrInvalidRegion      = errors.New("无效的地域")
	ErrInvalidResourceType = errors.New("无效的资源类型")

	// 适配器相关错误
	ErrAdapterNotFound     = errors.New("适配器不存在")
	ErrCredentialInvalid   = errors.New("凭证无效")
	ErrAPICallFailed       = errors.New("API调用失败")
	ErrDataConversionFailed = errors.New("数据转换失败")
)

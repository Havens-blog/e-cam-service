package errs

import "errors"

// ErrorCode 错误码定义
type ErrorCode struct {
	Code int
	Msg  string
}

// Error 实现 error 接口
func (e ErrorCode) Error() string {
	return e.Msg
}

var (
	// 通用错误码 (2xx, 4xx, 5xx)
	Success     = ErrorCode{Code: 200, Msg: "success"}
	ParamsError = ErrorCode{Code: 400, Msg: "params error"}
	SystemError = ErrorCode{Code: 500, Msg: "system error"}

	// 资产相关错误码 (404xxx, 409xxx, 400xxx, 500xxx)
	AssetNotFound      = ErrorCode{Code: 404001, Msg: "asset not found"}
	AssetAlreadyExist  = ErrorCode{Code: 409001, Msg: "asset already exist"}
	AssetTypeInvalid   = ErrorCode{Code: 400001, Msg: "asset type invalid"}
	ProviderNotSupport = ErrorCode{Code: 400002, Msg: "provider not support"}
	DiscoveryFailed    = ErrorCode{Code: 500001, Msg: "asset discovery failed"}

	// 账号相关错误码 (404xxx, 409xxx, 400xxx, 500xxx)
	AccountNotFound     = ErrorCode{Code: 404002, Msg: "cloud account not found"}
	AccountAlreadyExist = ErrorCode{Code: 409002, Msg: "cloud account already exist"}
	AccountDisabled     = ErrorCode{Code: 400003, Msg: "cloud account is disabled"}
	AccountAuthFailed   = ErrorCode{Code: 401001, Msg: "cloud account authentication failed"}
	AccountConnFailed   = ErrorCode{Code: 500002, Msg: "cloud account connection failed"}

	// 同步相关错误码 (409xxx, 500xxx)
	SyncInProgress = ErrorCode{Code: 409003, Msg: "sync is already in progress"}
	SyncFailed     = ErrorCode{Code: 500003, Msg: "sync failed"}

	// 权限相关错误码 (403xxx)
	InsufficientPermission = ErrorCode{Code: 403001, Msg: "insufficient permission"}
	ReadOnlyAccount        = ErrorCode{Code: 403002, Msg: "account is read-only"}

	// 配置相关错误码 (400xxx)
	ConfigInvalid  = ErrorCode{Code: 400004, Msg: "configuration invalid"}
	ConfigNotFound = ErrorCode{Code: 404003, Msg: "configuration not found"}

	// 任务相关错误码 (404xxx, 409xxx, 500xxx)
	TaskNotFound = ErrorCode{Code: 404004, Msg: "task not found"}
	TaskRunning  = ErrorCode{Code: 409004, Msg: "task is already running"}
	TaskFailed   = ErrorCode{Code: 500004, Msg: "task execution failed"}

	// 资源相关错误码 (404xxx, 409xxx, 500xxx)
	ResourceNotFound      = ErrorCode{Code: 404005, Msg: "resource not found"}
	ResourceUnavailable   = ErrorCode{Code: 503001, Msg: "resource temporarily unavailable"}
	ResourceQuotaExceeded = ErrorCode{Code: 429001, Msg: "resource quota exceeded"}
)

// 预定义的标准错误，用于不需要错误码的场景
var (
	ErrInvalidInput   = errors.New("invalid input")
	ErrTimeout        = errors.New("operation timeout")
	ErrCancelled      = errors.New("operation cancelled")
	ErrNotImplemented = errors.New("not implemented")
	ErrServiceBusy    = errors.New("service busy")
)

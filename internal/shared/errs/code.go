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
	ErrInvalidInput    = errors.New("invalid input")
	ErrTimeout         = errors.New("operation timeout")
	ErrCancelled       = errors.New("operation cancelled")
	ErrNotImplemented  = errors.New("not implemented")
	ErrServiceBusy     = errors.New("service busy")
	ErrInvalidAssetID  = errors.New("invalid asset id")
	ErrInvalidTenantID = errors.New("invalid tenant id")
)

// 模型相关错误码 (404xxx, 409xxx, 400xxx)
var (
	ModelNotFound     = ErrorCode{Code: 404006, Msg: "model not found"}
	ModelAlreadyExist = ErrorCode{Code: 409005, Msg: "model already exist"}
	ModelInvalid      = ErrorCode{Code: 400005, Msg: "model invalid"}
	FieldNotFound     = ErrorCode{Code: 404007, Msg: "field not found"}
	FieldAlreadyExist = ErrorCode{Code: 409006, Msg: "field already exist"}
	FieldInvalid      = ErrorCode{Code: 400006, Msg: "field invalid"}
	GroupNotFound     = ErrorCode{Code: 404008, Msg: "field group not found"}
	GroupAlreadyExist = ErrorCode{Code: 409007, Msg: "field group already exist"}
	RelationNotFound  = ErrorCode{Code: 404009, Msg: "relation not found"}
	RelationInvalid   = ErrorCode{Code: 400007, Msg: "relation invalid"}
)

// 模型相关标准错误
var (
	ErrInvalidModelUID      = errors.New("invalid model uid")
	ErrInvalidModelName     = errors.New("invalid model name")
	ErrInvalidModelCategory = errors.New("invalid model category")
	ErrInvalidFieldUID      = errors.New("invalid field uid")
	ErrInvalidFieldName     = errors.New("invalid field name")
	ErrInvalidFieldType     = errors.New("invalid field type")
	ErrInvalidGroupName     = errors.New("invalid group name")
	ErrCircularReference    = errors.New("circular reference detected")
	ErrInvalidRelation      = errors.New("invalid relation")
)

// IAM用户相关错误码 (404xxx, 409xxx, 400xxx, 500xxx)
var (
	UserNotFound      = ErrorCode{Code: 404010, Msg: "user not found"}
	UserAlreadyExists = ErrorCode{Code: 409008, Msg: "user already exists"}
	UserInvalidType   = ErrorCode{Code: 400008, Msg: "invalid user type"}
	UserSyncFailed    = ErrorCode{Code: 500005, Msg: "user sync failed"}
)

// IAM权限组相关错误码 (404xxx, 409xxx, 400xxx)
var (
	PermissionGroupNotFound      = ErrorCode{Code: 404011, Msg: "permission group not found"}
	PermissionGroupAlreadyExist  = ErrorCode{Code: 409009, Msg: "permission group already exists"}
	PermissionGroupHasUsers      = ErrorCode{Code: 400009, Msg: "permission group has users, cannot delete"}
	PermissionGroupPolicyInvalid = ErrorCode{Code: 400010, Msg: "permission policy invalid"}
)

// IAM同步任务相关错误码 (404xxx, 409xxx, 500xxx)
var (
	SyncTaskNotFound   = ErrorCode{Code: 404012, Msg: "sync task not found"}
	SyncTaskRunning    = ErrorCode{Code: 409010, Msg: "sync task is already running"}
	SyncTaskFailed     = ErrorCode{Code: 500006, Msg: "sync task execution failed"}
	SyncTaskTimeout    = ErrorCode{Code: 500007, Msg: "sync task timeout"}
	SyncTaskMaxRetries = ErrorCode{Code: 500008, Msg: "sync task reached max retries"}
)

// IAM模板相关错误码 (404xxx, 409xxx, 400xxx)
var (
	TemplateNotFound = ErrorCode{Code: 404013, Msg: "policy template not found"}
	TemplateBuiltIn  = ErrorCode{Code: 400011, Msg: "built-in template cannot be modified"}
	TemplateInUse    = ErrorCode{Code: 400012, Msg: "template is in use, cannot delete"}
)

// IAM云平台适配器错误码 (400xxx, 401xxx, 429xxx, 500xxx)
var (
	AdapterNotSupported = ErrorCode{Code: 400013, Msg: "cloud platform not supported"}
	AdapterAuthFailed   = ErrorCode{Code: 401002, Msg: "cloud platform authentication failed"}
	AdapterAPIError     = ErrorCode{Code: 500009, Msg: "cloud platform API error"}
	AdapterRateLimited  = ErrorCode{Code: 429002, Msg: "cloud platform API rate limited"}
)

// IAM标准错误
var (
	ErrInvalidUserType         = errors.New("invalid user type")
	ErrInvalidPermissionPolicy = errors.New("invalid permission policy")
	ErrSyncTaskNotRetryable    = errors.New("sync task not retryable")
	ErrTemplateNotEditable     = errors.New("template not editable")
)

// 租户相关错误码
var (
	TenantNotFound     = ErrorCode{Code: 404014, Msg: "tenant not found"}
	TenantAlreadyExist = ErrorCode{Code: 409011, Msg: "tenant already exists"}
	TenantDisabled     = ErrorCode{Code: 400014, Msg: "tenant is disabled"}
	TenantRequired     = ErrorCode{Code: 400015, Msg: "tenant id is required"}
)

// 实例相关错误码
var (
	InstanceNotFound     = ErrorCode{Code: 404015, Msg: "instance not found"}
	InstanceAlreadyExist = ErrorCode{Code: 409012, Msg: "instance already exists"}
)

// Tenant related error codes (from cam/errs/tenant.go)
var (
	// TenantNameAlreadyExist tenant name already exists
	TenantNameAlreadyExist = ErrorCode{Code: 409012, Msg: "tenant name already exists"}

	// TenantNotActive tenant is not active
	TenantNotActive = ErrorCode{Code: 403003, Msg: "tenant is not active"}

	// TenantHasResources tenant has associated resources, cannot delete
	TenantHasResources = ErrorCode{Code: 400014, Msg: "tenant has associated resources, cannot delete"}

	// TenantCloudAccountLimitExceeded cloud account count exceeds tenant quota
	TenantCloudAccountLimitExceeded = ErrorCode{Code: 429003, Msg: "cloud account count exceeds tenant quota"}

	// TenantUserLimitExceeded user count exceeds tenant quota
	TenantUserLimitExceeded = ErrorCode{Code: 429004, Msg: "user count exceeds tenant quota"}

	// TenantUserGroupLimitExceeded user group count exceeds tenant quota
	TenantUserGroupLimitExceeded = ErrorCode{Code: 429005, Msg: "user group count exceeds tenant quota"}

	// TenantProviderNotAllowed cloud provider not in tenant allowed list
	TenantProviderNotAllowed = ErrorCode{Code: 403004, Msg: "cloud provider not in tenant allowed list"}

	// TenantFeatureNotEnabled tenant feature not enabled
	TenantFeatureNotEnabled = ErrorCode{Code: 403005, Msg: "tenant feature not enabled"}
)

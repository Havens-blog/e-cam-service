package errs

// Tenant related error codes (404xxx, 409xxx, 403xxx, 429xxx)
var (
	// TenantNotFound tenant not found
	TenantNotFound = ErrorCode{Code: 404014, Msg: "tenant not found"}

	// TenantAlreadyExist tenant ID already exists
	TenantAlreadyExist = ErrorCode{Code: 409011, Msg: "tenant ID already exists"}

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

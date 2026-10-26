package errs

// 租户治理强制校验相关错误码（403xxx, 429xxx, 503xxx）。
// CreateAccount 落库前强制校验租户云厂商白名单（R7）与云账号配额（R2.4），
// 数据源为 eiam 租户治理配置；查询失败一律 fail-closed 拒绝，不静默放行。
var (
	// ErrVendorNotAllowed 云厂商不在租户 AllowedVendors 白名单内（R7）
	ErrVendorNotAllowed = ErrorCode{Code: 403003, Msg: "cloud vendor not allowed for tenant"}

	// ErrTenantQuotaExceeded 租户云账号数已达 MaxAccounts 上限（R2.4）
	ErrTenantQuotaExceeded = ErrorCode{Code: 429003, Msg: "tenant cloud account quota exceeded"}

	// ErrTenantGovernanceUnavailable 租户治理配置查询失败（不可达/超时/租户不存在），fail-closed
	ErrTenantGovernanceUnavailable = ErrorCode{Code: 503002, Msg: "tenant governance config unavailable"}
)

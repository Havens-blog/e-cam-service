package errs

import "errors"

// 实例相关错误码 (404xxx, 409xxx, 400xxx)
var (
	InstanceNotFound = ErrorCode{Code: 404014, Msg: "instance not found"}
	InstanceExists   = ErrorCode{Code: 409011, Msg: "instance already exists"}
	InstanceInvalid  = ErrorCode{Code: 400014, Msg: "instance invalid"}
)

// 实例相关标准错误
var (
	ErrInvalidAssetID    = errors.New("asset id cannot be empty")
	ErrInvalidTenantID   = errors.New("tenant id cannot be empty")
	ErrInstanceNotFound  = errors.New("instance not found")
	ErrInstanceExists    = errors.New("instance already exists")
	ErrInvalidAttributes = errors.New("invalid attributes")
)

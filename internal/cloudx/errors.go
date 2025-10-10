package cloudx

import "errors"

var (
	// ErrUnsupportedProvider 不支持的云厂商
	ErrUnsupportedProvider = errors.New("unsupported cloud provider")

	// ErrInvalidCredentials 无效的凭证
	ErrInvalidCredentials = errors.New("invalid credentials")

	// ErrConnectionTimeout 连接超时
	ErrConnectionTimeout = errors.New("connection timeout")

	// ErrPermissionDenied 权限不足
	ErrPermissionDenied = errors.New("permission denied")

	// ErrRegionNotSupported 地域不支持
	ErrRegionNotSupported = errors.New("region not supported")
)

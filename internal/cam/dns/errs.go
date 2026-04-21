package dns

import "github.com/Havens-blog/e-cam-service/internal/cam/errs"

// DNS 模块错误码
var (
	ErrDNSRecordInvalid    = errs.ErrorCode{Code: 400050, Msg: "invalid DNS record"}
	ErrDNSRecordRREmpty    = errs.ErrorCode{Code: 400051, Msg: "record RR cannot be empty"}
	ErrDNSRecordValueEmpty = errs.ErrorCode{Code: 400052, Msg: "record value cannot be empty"}
	ErrDNSRecordTTLRange   = errs.ErrorCode{Code: 400053, Msg: "TTL must be between 1 and 86400"}
	ErrDNSMXPriority       = errs.ErrorCode{Code: 400054, Msg: "MX priority must be between 1 and 65535"}
	ErrDNSInvalidIPv4      = errs.ErrorCode{Code: 400055, Msg: "invalid IPv4 address"}
	ErrDNSInvalidIPv6      = errs.ErrorCode{Code: 400056, Msg: "invalid IPv6 address"}
	ErrDNSInvalidDomain    = errs.ErrorCode{Code: 400057, Msg: "invalid domain name format"}
	ErrDNSTXTTooLong       = errs.ErrorCode{Code: 400058, Msg: "TXT record value exceeds 512 characters"}
	ErrDNSCloudAPIFailed   = errs.ErrorCode{Code: 500050, Msg: "cloud DNS API call failed"}
	ErrDNSAccountNotFound  = errs.ErrorCode{Code: 404050, Msg: "cloud account not found"}
)

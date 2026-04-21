package dns

import (
	"net"
	"regexp"
	"strings"
)

// domainRegex 域名格式正则：支持标准域名和以 . 结尾的 FQDN
var domainRegex = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}\.?$`)

// ValidateRecord 校验解析记录字段合法性（主入口）
func ValidateRecord(recordType, rr, value string, ttl, priority int) error {
	if strings.TrimSpace(rr) == "" {
		return ErrDNSRecordRREmpty
	}
	if strings.TrimSpace(value) == "" {
		return ErrDNSRecordValueEmpty
	}
	if err := ValidateTTL(ttl); err != nil {
		return err
	}
	if !ValidRecordTypes[recordType] {
		return ErrDNSRecordInvalid
	}

	switch recordType {
	case "A":
		return ValidateIPv4(value)
	case "AAAA":
		return ValidateIPv6(value)
	case "CNAME", "NS":
		return ValidateDomainName(value)
	case "MX":
		if err := ValidateDomainName(value); err != nil {
			return err
		}
		return ValidateMXPriority(priority)
	case "TXT":
		return ValidateTXTLength(value)
	case "SRV":
		return ValidateDomainName(value)
	case "CAA":
		// CAA 记录值格式较自由，仅做非空校验（已在上方完成）
		return nil
	}
	return nil
}

// ValidateIPv4 校验 IPv4 地址格式
func ValidateIPv4(value string) error {
	ip := net.ParseIP(value)
	if ip == nil || ip.To4() == nil {
		return ErrDNSInvalidIPv4
	}
	return nil
}

// ValidateIPv6 校验 IPv6 地址格式
func ValidateIPv6(value string) error {
	ip := net.ParseIP(value)
	if ip == nil || ip.To4() != nil {
		return ErrDNSInvalidIPv6
	}
	return nil
}

// ValidateDomainName 校验域名格式
func ValidateDomainName(value string) error {
	if !domainRegex.MatchString(value) {
		return ErrDNSInvalidDomain
	}
	return nil
}

// ValidateTTL 校验 TTL 范围 [1, 86400]
func ValidateTTL(ttl int) error {
	if ttl < 1 || ttl > 86400 {
		return ErrDNSRecordTTLRange
	}
	return nil
}

// ValidateMXPriority 校验 MX 优先级范围 [1, 65535]
func ValidateMXPriority(priority int) error {
	if priority < 1 || priority > 65535 {
		return ErrDNSMXPriority
	}
	return nil
}

// ValidateTXTLength 校验 TXT 记录值长度 <= 512
func ValidateTXTLength(value string) error {
	if len(value) > 512 {
		return ErrDNSTXTTooLong
	}
	return nil
}

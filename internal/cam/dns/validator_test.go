package dns

import (
	"errors"
	"strings"
	"testing"
)

// ==================== ValidateRecord 集成测试 ====================

func TestValidateRecord_RREmpty(t *testing.T) {
	tests := []struct {
		name string
		rr   string
	}{
		{"empty string", ""},
		{"spaces only", "   "},
		{"tab only", "\t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRecord("A", tt.rr, "1.2.3.4", 600, 0)
			if !errors.Is(err, ErrDNSRecordRREmpty) {
				t.Errorf("expected ErrDNSRecordRREmpty, got %v", err)
			}
		})
	}
}

func TestValidateRecord_ValueEmpty(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"empty string", ""},
		{"spaces only", "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRecord("A", "www", tt.value, 600, 0)
			if !errors.Is(err, ErrDNSRecordValueEmpty) {
				t.Errorf("expected ErrDNSRecordValueEmpty, got %v", err)
			}
		})
	}
}

func TestValidateRecord_InvalidRecordType(t *testing.T) {
	err := ValidateRecord("INVALID", "www", "1.2.3.4", 600, 0)
	if !errors.Is(err, ErrDNSRecordInvalid) {
		t.Errorf("expected ErrDNSRecordInvalid, got %v", err)
	}
}

func TestValidateRecord_TTLOutOfRange(t *testing.T) {
	tests := []struct {
		name string
		ttl  int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too large", 86401},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRecord("A", "www", "1.2.3.4", tt.ttl, 0)
			if !errors.Is(err, ErrDNSRecordTTLRange) {
				t.Errorf("expected ErrDNSRecordTTLRange, got %v", err)
			}
		})
	}
}

func TestValidateRecord_A_Valid(t *testing.T) {
	if err := ValidateRecord("A", "www", "192.168.1.1", 600, 0); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateRecord_A_Invalid(t *testing.T) {
	err := ValidateRecord("A", "www", "not-an-ip", 600, 0)
	if !errors.Is(err, ErrDNSInvalidIPv4) {
		t.Errorf("expected ErrDNSInvalidIPv4, got %v", err)
	}
}

func TestValidateRecord_A_IPv6AsIPv4(t *testing.T) {
	err := ValidateRecord("A", "www", "::1", 600, 0)
	if !errors.Is(err, ErrDNSInvalidIPv4) {
		t.Errorf("expected ErrDNSInvalidIPv4, got %v", err)
	}
}

func TestValidateRecord_AAAA_Valid(t *testing.T) {
	if err := ValidateRecord("AAAA", "www", "2001:db8::1", 600, 0); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateRecord_AAAA_Invalid(t *testing.T) {
	err := ValidateRecord("AAAA", "www", "not-ipv6", 600, 0)
	if !errors.Is(err, ErrDNSInvalidIPv6) {
		t.Errorf("expected ErrDNSInvalidIPv6, got %v", err)
	}
}

func TestValidateRecord_AAAA_IPv4AsIPv6(t *testing.T) {
	err := ValidateRecord("AAAA", "www", "1.2.3.4", 600, 0)
	if !errors.Is(err, ErrDNSInvalidIPv6) {
		t.Errorf("expected ErrDNSInvalidIPv6, got %v", err)
	}
}

func TestValidateRecord_CNAME_Valid(t *testing.T) {
	if err := ValidateRecord("CNAME", "www", "example.com", 600, 0); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateRecord_CNAME_Invalid(t *testing.T) {
	err := ValidateRecord("CNAME", "www", "not a domain!", 600, 0)
	if !errors.Is(err, ErrDNSInvalidDomain) {
		t.Errorf("expected ErrDNSInvalidDomain, got %v", err)
	}
}

func TestValidateRecord_MX_Valid(t *testing.T) {
	if err := ValidateRecord("MX", "mail", "mail.example.com", 600, 10); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateRecord_MX_InvalidDomain(t *testing.T) {
	err := ValidateRecord("MX", "mail", "bad domain!", 600, 10)
	if !errors.Is(err, ErrDNSInvalidDomain) {
		t.Errorf("expected ErrDNSInvalidDomain, got %v", err)
	}
}

func TestValidateRecord_MX_PriorityOutOfRange(t *testing.T) {
	tests := []struct {
		name     string
		priority int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too large", 65536},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRecord("MX", "mail", "mail.example.com", 600, tt.priority)
			if !errors.Is(err, ErrDNSMXPriority) {
				t.Errorf("expected ErrDNSMXPriority, got %v", err)
			}
		})
	}
}

func TestValidateRecord_TXT_Valid(t *testing.T) {
	if err := ValidateRecord("TXT", "@", "v=spf1 include:example.com ~all", 600, 0); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateRecord_TXT_TooLong(t *testing.T) {
	longValue := strings.Repeat("a", 513)
	err := ValidateRecord("TXT", "@", longValue, 600, 0)
	if !errors.Is(err, ErrDNSTXTTooLong) {
		t.Errorf("expected ErrDNSTXTTooLong, got %v", err)
	}
}

func TestValidateRecord_TXT_ExactLimit(t *testing.T) {
	exactValue := strings.Repeat("a", 512)
	if err := ValidateRecord("TXT", "@", exactValue, 600, 0); err != nil {
		t.Errorf("expected nil for 512 chars, got %v", err)
	}
}

func TestValidateRecord_NS_Valid(t *testing.T) {
	if err := ValidateRecord("NS", "@", "ns1.example.com", 600, 0); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateRecord_SRV_Valid(t *testing.T) {
	if err := ValidateRecord("SRV", "_sip._tcp", "sip.example.com", 600, 0); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateRecord_CAA_Valid(t *testing.T) {
	if err := ValidateRecord("CAA", "@", "0 issue letsencrypt.org", 600, 0); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateRecord_TTLBoundary(t *testing.T) {
	// TTL = 1 (min valid)
	if err := ValidateRecord("A", "www", "1.2.3.4", 1, 0); err != nil {
		t.Errorf("TTL=1 should be valid, got %v", err)
	}
	// TTL = 86400 (max valid)
	if err := ValidateRecord("A", "www", "1.2.3.4", 86400, 0); err != nil {
		t.Errorf("TTL=86400 should be valid, got %v", err)
	}
}

func TestValidateRecord_MXPriorityBoundary(t *testing.T) {
	// Priority = 1 (min valid)
	if err := ValidateRecord("MX", "mail", "mail.example.com", 600, 1); err != nil {
		t.Errorf("priority=1 should be valid, got %v", err)
	}
	// Priority = 65535 (max valid)
	if err := ValidateRecord("MX", "mail", "mail.example.com", 600, 65535); err != nil {
		t.Errorf("priority=65535 should be valid, got %v", err)
	}
}

// ==================== 独立校验函数测试 ====================

func TestValidateIPv4(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{"1.2.3.4", true},
		{"0.0.0.0", true},
		{"255.255.255.255", true},
		{"10.0.0.1", true},
		{"abc", false},
		{"::1", false},
		{"999.999.999.999", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			err := ValidateIPv4(tt.value)
			if tt.valid && err != nil {
				t.Errorf("expected valid, got %v", err)
			}
			if !tt.valid && err == nil {
				t.Errorf("expected error for %q", tt.value)
			}
		})
	}
}

func TestValidateIPv6(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{"::1", true},
		{"2001:db8::1", true},
		{"fe80::1%eth0", false},
		{"1.2.3.4", false},
		{"not-ipv6", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			err := ValidateIPv6(tt.value)
			if tt.valid && err != nil {
				t.Errorf("expected valid, got %v", err)
			}
			if !tt.valid && err == nil {
				t.Errorf("expected error for %q", tt.value)
			}
		})
	}
}

func TestValidateDomainName(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{"example.com", true},
		{"sub.example.com", true},
		{"example.com.", true},
		{"a.b.c.d.example.com", true},
		{"not a domain", false},
		{"-invalid.com", false},
		{"", false},
		{"a", false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			err := ValidateDomainName(tt.value)
			if tt.valid && err != nil {
				t.Errorf("expected valid for %q, got %v", tt.value, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("expected error for %q", tt.value)
			}
		})
	}
}

func TestValidateTTL(t *testing.T) {
	tests := []struct {
		ttl   int
		valid bool
	}{
		{1, true},
		{600, true},
		{86400, true},
		{0, false},
		{-1, false},
		{86401, false},
	}
	for _, tt := range tests {
		err := ValidateTTL(tt.ttl)
		if tt.valid && err != nil {
			t.Errorf("TTL=%d expected valid, got %v", tt.ttl, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("TTL=%d expected error", tt.ttl)
		}
	}
}

func TestValidateMXPriority(t *testing.T) {
	tests := []struct {
		priority int
		valid    bool
	}{
		{1, true},
		{10, true},
		{65535, true},
		{0, false},
		{-1, false},
		{65536, false},
	}
	for _, tt := range tests {
		err := ValidateMXPriority(tt.priority)
		if tt.valid && err != nil {
			t.Errorf("priority=%d expected valid, got %v", tt.priority, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("priority=%d expected error", tt.priority)
		}
	}
}

func TestValidateTXTLength(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{"short", "hello", true},
		{"exact 512", strings.Repeat("x", 512), true},
		{"513 chars", strings.Repeat("x", 513), false},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTXTLength(tt.value)
			if tt.valid && err != nil {
				t.Errorf("expected valid, got %v", err)
			}
			if !tt.valid && err == nil {
				t.Errorf("expected error")
			}
		})
	}
}

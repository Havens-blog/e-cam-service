package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Havens-blog/e-cloudx-sdk/domain"
)

// TestClassifyProbeError 权限探测错误归类：403/AccessDenied 类必须归为 denied，
// 其余网络类错误归为 error，成功为 ok。
func TestClassifyProbeError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus string
		wantCode   string
	}{
		{"nil 成功", nil, "ok", ""},
		{"Forbidden 权限不足", errors.New("SDK.ServerError ErrorCode: Forbidden"), "denied", "Forbidden"},
		{"AccessDenied 权限不足", errors.New("AccessDenied: no permission"), "denied", "AccessDenied"},
		{"NoPermission 权限不足", errors.New("NoPermission"), "denied", "NoPermission"},
		{"InvalidAccessKeyId 视为凭证无效", errors.New("InvalidAccessKeyId.NotFound"), "denied", "InvalidAccessKeyId"},
		{"网络超时归为 error", errors.New("dial tcp: i/o timeout"), "error", ""},
		{"限流归为 error", errors.New("Throttling.User"), "error", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, code, _ := classifyProbeError(tc.err)
			if status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", status, tc.wantStatus)
			}
			if code != tc.wantCode {
				t.Fatalf("code = %q, want %q", code, tc.wantCode)
			}
		})
	}
}

// TestClassifyProbeErrorTruncates 超长错误信息必须被截断，避免整段堆栈落库
func TestClassifyProbeErrorTruncates(t *testing.T) {
	long := strings.Repeat("x", 1000)
	_, _, msg := classifyProbeError(errors.New(long))
	if len(msg) > 400 {
		t.Fatalf("message not truncated, len=%d", len(msg))
	}
}

// TestResolveAliyunRegion 地域解析：优先账号配置地域，缺省回退 cn-hangzhou
func TestResolveAliyunRegion(t *testing.T) {
	acc := &domain.CloudAccount{Regions: []string{"cn-shanghai", "cn-beijing"}}
	if got := resolveAliyunRegion(acc); got != "cn-shanghai" {
		t.Fatalf("got %q, want cn-shanghai", got)
	}

	empty := &domain.CloudAccount{}
	if got := resolveAliyunRegion(empty); got != "cn-hangzhou" {
		t.Fatalf("got %q, want cn-hangzhou", got)
	}

	cfgOnly := &domain.CloudAccount{Config: domain.CloudAccountConfig{SupportedRegions: []string{"cn-shenzhen"}}}
	if got := resolveAliyunRegion(cfgOnly); got != "cn-shenzhen" {
		t.Fatalf("got %q, want cn-shenzhen", got)
	}
}

// TestAssertNotPrimaryAccountSkipsNonAliyun 非阿里云账号不做主账号校验（不发起网络调用）
func TestAssertNotPrimaryAccountSkipsNonAliyun(t *testing.T) {
	svc := &cloudAccountService{}
	acc := &domain.CloudAccount{Provider: domain.CloudProviderHuawei}
	if err := svc.assertNotPrimaryAccount(context.Background(), acc); err != nil {
		t.Fatalf("unexpected error for non-aliyun provider: %v", err)
	}
}

// TestPrimaryAccountForbiddenMessage 拒绝文案必须包含关键信息
func TestPrimaryAccountForbiddenMessage(t *testing.T) {
	if !strings.Contains(errPrimaryAccountForbidden, "主账号") {
		t.Fatalf("message must mention 主账号, got %q", errPrimaryAccountForbidden)
	}
	if !strings.Contains(errPrimaryAccountForbidden, "RAM 子账号") {
		t.Fatalf("message must mention RAM 子账号, got %q", errPrimaryAccountForbidden)
	}
}

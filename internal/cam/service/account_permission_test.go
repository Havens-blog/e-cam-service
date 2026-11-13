package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Havens-blog/e-cloudx-sdk/domain"
)

const (
	testAK = "LTAI5tTESTKEY1234567890AB"
	testSK = "testSecretValue1234567890abcdef"
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
			status, code, _ := classifyProbeError(tc.err, testAK, testSK)
			if status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", status, tc.wantStatus)
			}
			if code != tc.wantCode {
				t.Fatalf("code = %q, want %q", code, tc.wantCode)
			}
		})
	}
}

// TestClassifyProbeErrorRedactsSecrets 阿里云 SDK 报错会带上完整签名 URL，
// 落库/回显前必须抹掉 AccessKeyId、Secret 与查询串。
func TestClassifyProbeErrorRedactsSecrets(t *testing.T) {
	raw := `Post "https://slb.aliyuncs.com/?AccessKeyId=` + testAK + `&Signature=abc&AccessKeySecret=` + testSK + `": dial tcp: i/o timeout`
	_, _, msg := classifyProbeError(errors.New(raw), testAK, testSK)

	if strings.Contains(msg, testAK) {
		t.Fatalf("message leaks AccessKeyId: %s", msg)
	}
	if strings.Contains(msg, testSK) {
		t.Fatalf("message leaks AccessKeySecret: %s", msg)
	}
	if strings.Contains(msg, "Signature=") || strings.Contains(msg, "AccessKeyId=") {
		t.Fatalf("message leaks signed query: %s", msg)
	}
	if !strings.Contains(msg, "i/o timeout") {
		t.Fatalf("message should keep the underlying reason: %s", msg)
	}
}

// TestClassifyProbeErrorTruncates 超长错误信息必须被截断，避免整段堆栈落库
func TestClassifyProbeErrorTruncates(t *testing.T) {
	long := strings.Repeat("x", 1000)
	_, _, msg := classifyProbeError(errors.New(long), "", "")
	if len(msg) > 400 {
		t.Fatalf("message not truncated, len=%d", len(msg))
	}
}

// TestIsTransientProbeError 网络类抖动应被识别为可重试
func TestIsTransientProbeError(t *testing.T) {
	if !isTransientProbeError(errors.New("dial tcp 1.2.3.4:443: i/o timeout")) {
		t.Fatal("i/o timeout should be transient")
	}
	if !isTransientProbeError(errors.New("connection reset by peer")) {
		t.Fatal("connection reset should be transient")
	}
	if isTransientProbeError(errors.New("Forbidden")) {
		t.Fatal("Forbidden must not be treated as transient")
	}
	if isTransientProbeError(nil) {
		t.Fatal("nil must not be transient")
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

// TestPrimaryAccountRejection 主账号身份必须被拒绝（用身份串驱动，不依赖线上 STS）
func TestPrimaryAccountRejection(t *testing.T) {
	if err := rejectPrimaryIdentity("PrimaryAccount"); err == nil {
		t.Fatal("PrimaryAccount must be rejected")
	} else if !strings.Contains(err.Error(), "禁止接入主账号") {
		t.Fatalf("unexpected message: %v", err)
	}

	for _, ok := range []string{"RAMUser", "RAMRole", ""} {
		if err := rejectPrimaryIdentity(ok); err != nil {
			t.Fatalf("identity %q must be allowed, got %v", ok, err)
		}
	}
}

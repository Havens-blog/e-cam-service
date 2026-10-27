package governance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/auth"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/suite"
)

// fakeTokenProvider 测试用令牌换取器：按预置行为返回 token 或 error，并记录调用。
type fakeTokenProvider struct {
	token string
	err   error
	calls []int64
}

func (f *fakeTokenProvider) TokenFor(_ context.Context, tenantID int64) (string, error) {
	f.calls = append(f.calls, tenantID)
	if f.err != nil {
		return "", f.err
	}
	return f.token, nil
}

// EIAMTenantClientTestSuite eiam 租户治理配置 HTTP 客户端测试。
// 经 httptest 起假 eiam 端点验证契约（信封解析/鉴权头/fail-closed），
// 不连真实 eiam——真实跨服务联调不在本任务验收范围（mock 验证口径）。
type EIAMTenantClientTestSuite struct {
	suite.Suite
}

func (suite *EIAMTenantClientTestSuite) TestGetGovernance() {
	tests := []struct {
		name           string
		tokenProvider  auth.TokenProvider
		handler        http.HandlerFunc
		tenantID       int64
		expectAccounts int64
		expectVendors  []string
		expectErr      bool
	}{
		{
			name:          "成功解析治理配置并携带 Bearer token",
			tokenProvider: &fakeTokenProvider{token: "svc-token-1"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				suite.Equal(http.MethodGet, r.Method)
				suite.Equal("/api/tenant/detail/8", r.URL.Path)
				suite.Equal("Bearer svc-token-1", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"id":8,"name":"t","max_accounts":3,"allowed_vendors":["aliyun","aws"]}}`))
			},
			tenantID:       8,
			expectAccounts: 3,
			expectVendors:  []string{"aliyun", "aws"},
		},
		{
			name:          "换取服务令牌失败 fail-closed",
			tokenProvider: &fakeTokenProvider{err: errors.New("token endpoint down")},
			handler:       func(w http.ResponseWriter, r *http.Request) { suite.Fail("不应发起详情请求") },
			tenantID:      8,
			expectErr:     true,
		},
		{
			name:          "tokenProvider 未装配 fail-closed",
			tokenProvider: nil,
			handler:       func(w http.ResponseWriter, r *http.Request) { suite.Fail("不应发起详情请求") },
			tenantID:      8,
			expectErr:     true,
		},
		{
			name:          "零值 data 按无约束透传（配额 0、白名单空）",
			tokenProvider: &fakeTokenProvider{token: "t"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"id":8}}`))
			},
			tenantID:       8,
			expectAccounts: 0,
			expectVendors:  nil,
		},
		{
			name:          "HTTP 401（服务令牌无效）fail-closed",
			tokenProvider: &fakeTokenProvider{token: "t"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			tenantID:  8,
			expectErr: true,
		},
		{
			name:          "HTTP 500（业务失败如租户不存在）fail-closed",
			tokenProvider: &fakeTokenProvider{token: "t"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"code":4020906,"msg":"获取租户空间详情失败","data":null}`))
			},
			tenantID:  8,
			expectErr: true,
		},
		{
			name:          "HTTP 200 但信封 code 非 0 fail-closed",
			tokenProvider: &fakeTokenProvider{token: "t"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"code":4020906,"msg":"获取租户空间详情失败","data":null}`))
			},
			tenantID:  8,
			expectErr: true,
		},
		{
			name:          "响应体非 JSON fail-closed",
			tokenProvider: &fakeTokenProvider{token: "t"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`<html>gateway error</html>`))
			},
			tenantID:  8,
			expectErr: true,
		},
		{
			name:          "非法租户 ID 不发起请求也不换 token fail-closed",
			tokenProvider: &fakeTokenProvider{token: "t"},
			handler:       func(w http.ResponseWriter, r *http.Request) { suite.Fail("不应发起 HTTP 请求") },
			tenantID:      0,
			expectErr:     true,
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := NewEIAMTenantClient(server.URL, tt.tokenProvider, elog.DefaultLogger)
			maxAccounts, vendors, err := client.GetGovernance(context.Background(), tt.tenantID)

			if tt.expectErr {
				suite.Error(err)
				suite.Equal(int64(0), maxAccounts)
				suite.Nil(vendors)
			} else {
				suite.NoError(err)
				suite.Equal(tt.expectAccounts, maxAccounts)
				suite.Equal(tt.expectVendors, vendors)
			}
		})
	}
}

// TestBaseURLEmptyFailClosed base_url 未配置时查询报错（上层 fail-closed），构造期不 panic
func (suite *EIAMTenantClientTestSuite) TestBaseURLEmptyFailClosed() {
	client := NewEIAMTenantClient("", &fakeTokenProvider{token: "t"}, elog.DefaultLogger)

	maxAccounts, vendors, err := client.GetGovernance(context.Background(), 8)

	suite.Error(err)
	suite.True(errors.Is(err, errBaseURLNotConfigured))
	suite.Equal(int64(0), maxAccounts)
	suite.Nil(vendors)
}

// TestServerUnreachableFailClosed 目标不可达 fail-closed
func (suite *EIAMTenantClientTestSuite) TestServerUnreachableFailClosed() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // 立即关闭制造不可达

	client := NewEIAMTenantClient(server.URL, &fakeTokenProvider{token: "t"}, elog.DefaultLogger)

	_, _, err := client.GetGovernance(context.Background(), 8)

	suite.Error(err)
}

func TestEIAMTenantClientSuite(t *testing.T) {
	suite.Run(t, new(EIAMTenantClientTestSuite))
}

package governance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/suite"
)

// EIAMTenantClientTestSuite eiam 租户治理配置 HTTP 客户端测试。
// 经 httptest 起假 eiam 端点验证契约（信封解析/鉴权头/fail-closed），
// 不连真实 eiam——真实跨服务联调不在本任务验收范围（mock 验证口径）。
type EIAMTenantClientTestSuite struct {
	suite.Suite
}

// newFakeEIAM 起一个按 handler 响应的假 eiam，返回服务器与收到的请求记录
func (suite *EIAMTenantClientTestSuite) newFakeEIAM(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func (suite *EIAMTenantClientTestSuite) TestGetGovernance() {
	tests := []struct {
		name           string
		token          string
		handler        http.HandlerFunc
		tenantID       int64
		expectAccounts int64
		expectVendors  []string
		expectErr      bool
	}{
		{
			name:  "成功解析治理配置并携带 Bearer token",
			token: "svc-token-1",
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
			name:  "未配置 token 时不发送 Authorization 头",
			token: "",
			handler: func(w http.ResponseWriter, r *http.Request) {
				suite.Empty(r.Header.Get("Authorization"))
				_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"max_accounts":5}}`))
			},
			tenantID:       8,
			expectAccounts: 5,
			expectVendors:  nil,
		},
		{
			name:  "零值 data 按无约束透传（配额 0、白名单空）",
			token: "t",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"id":8}}`))
			},
			tenantID:       8,
			expectAccounts: 0,
			expectVendors:  nil,
		},
		{
			name:  "HTTP 401（无会话）fail-closed",
			token: "t",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			tenantID:  8,
			expectErr: true,
		},
		{
			name:  "HTTP 500（业务失败如租户不存在）fail-closed",
			token: "t",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"code":4020906,"msg":"获取租户空间详情失败","data":null}`))
			},
			tenantID:  8,
			expectErr: true,
		},
		{
			name:  "HTTP 200 但信封 code 非 0 fail-closed",
			token: "t",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"code":4020906,"msg":"获取租户空间详情失败","data":null}`))
			},
			tenantID:  8,
			expectErr: true,
		},
		{
			name:  "响应体非 JSON fail-closed",
			token: "t",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`<html>gateway error</html>`))
			},
			tenantID:  8,
			expectErr: true,
		},
		{
			name:      "非法租户 ID 不发起请求 fail-closed",
			token:     "t",
			handler:   func(w http.ResponseWriter, r *http.Request) { suite.Fail("不应发起 HTTP 请求") },
			tenantID:  0,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			server := suite.newFakeEIAM(tt.handler)
			defer server.Close()

			client := NewEIAMTenantClient(server.URL, tt.token, elog.DefaultLogger)
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
	client := NewEIAMTenantClient("", "t", elog.DefaultLogger)

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

	client := NewEIAMTenantClient(server.URL, "t", elog.DefaultLogger)

	_, _, err := client.GetGovernance(context.Background(), 8)

	suite.Error(err)
}

func TestEIAMTenantClientSuite(t *testing.T) {
	suite.Run(t, new(EIAMTenantClientTestSuite))
}

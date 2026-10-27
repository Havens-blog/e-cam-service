package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServiceTokenProvider_TokenFor 覆盖换取/缓存/fail-closed 分支。
func TestServiceTokenProvider_TokenFor(t *testing.T) {
	var (
		mu       sync.Mutex
		reqCount int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqCount++
		mu.Unlock()

		_ = r.ParseForm()
		assert.Equal(t, "client_credentials", r.PostForm.Get("grant_type"))
		assert.Equal(t, "svc-client", r.PostForm.Get("client_id"))
		assert.Equal(t, "svc-secret", r.PostForm.Get("client_secret"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-abc","token_type":"Bearer","expires_in":900}`))
	}))
	defer server.Close()

	provider := NewServiceTokenProvider(server.URL, "svc-client", "svc-secret", elog.DefaultLogger)

	// 首次换取成功
	tok, err := provider.TokenFor(context.Background(), 3)
	require.NoError(t, err)
	assert.Equal(t, "tok-abc", tok)

	// 缓存命中:不重复调 token 端点
	tok2, err := provider.TokenFor(context.Background(), 3)
	require.NoError(t, err)
	assert.Equal(t, "tok-abc", tok2)

	mu.Lock()
	assert.Equal(t, 1, reqCount, "同一租户第二次应命中缓存")
	mu.Unlock()

	// 不同租户:重新换取
	tok3, err := provider.TokenFor(context.Background(), 5)
	require.NoError(t, err)
	assert.Equal(t, "tok-abc", tok3)
	mu.Lock()
	assert.Equal(t, 2, reqCount)
	mu.Unlock()
}

// TestServiceTokenProvider_NoTenant 无租户(tenantID=0)换取纯服务身份 token。
func TestServiceTokenProvider_NoTenant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "0", r.PostForm.Get("tenant_id"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-svc","expires_in":900}`))
	}))
	defer server.Close()

	provider := NewServiceTokenProvider(server.URL, "c", "s", elog.DefaultLogger)
	tok, err := provider.TokenFor(context.Background(), 0)
	require.NoError(t, err)
	assert.Equal(t, "tok-svc", tok)
}

func TestServiceTokenProvider_FailClosed(t *testing.T) {
	t.Run("非法租户 ID", func(t *testing.T) {
		provider := NewServiceTokenProvider("http://x", "c", "s", elog.DefaultLogger)
		_, err := provider.TokenFor(context.Background(), -1)
		require.Error(t, err)
	})

	t.Run("token_url 未配置", func(t *testing.T) {
		provider := NewServiceTokenProvider("", "c", "s", elog.DefaultLogger)
		_, err := provider.TokenFor(context.Background(), 3)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "token_url")
	})

	t.Run("client 凭证未配置", func(t *testing.T) {
		provider := NewServiceTokenProvider("http://x", "", "", elog.DefaultLogger)
		_, err := provider.TokenFor(context.Background(), 3)
		require.Error(t, err)
	})

	t.Run("token 端点返回非 200", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()
		provider := NewServiceTokenProvider(server.URL, "c", "s", elog.DefaultLogger)
		_, err := provider.TokenFor(context.Background(), 3)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "401")
	})

	t.Run("token 端点不可达", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		server.Close()
		provider := NewServiceTokenProvider(server.URL, "c", "s", elog.DefaultLogger)
		_, err := provider.TokenFor(context.Background(), 3)
		require.Error(t, err)
	})
}

// TestServiceTokenProvider_RefreshAfterExpiry 缓存到期后重新换取。
func TestServiceTokenProvider_RefreshAfterExpiry(t *testing.T) {
	var reqCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-new","token_type":"Bearer","expires_in":2}`))
	}))
	defer server.Close()

	provider := NewServiceTokenProvider(server.URL, "c", "s", elog.DefaultLogger)
	_, err := provider.TokenFor(context.Background(), 3)
	require.NoError(t, err)

	// expires_in=2s,提前 60s 刷新 => 缓存 expiry 被退化为半衰期(1s);等待后应重新换取
	time.Sleep(1100 * time.Millisecond)
	_, err = provider.TokenFor(context.Background(), 3)
	require.NoError(t, err)

	assert.Equal(t, 2, reqCount, "缓存过期后应重新换取")
}

// 确认请求体为 application/x-www-form-urlencoded 且包含 tenant_id。
func TestServiceTokenProvider_RequestBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		_ = r.ParseForm()
		assert.Equal(t, "3", r.PostForm.Get("tenant_id"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":900}`))
	}))
	defer server.Close()

	provider := NewServiceTokenProvider(server.URL, "c", "s", elog.DefaultLogger)
	tok, err := provider.TokenFor(context.Background(), 3)
	require.NoError(t, err)
	assert.NotEmpty(t, tok)
	assert.True(t, strings.Contains(tok, "tok"))
}

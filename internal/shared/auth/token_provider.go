// Package auth 平台统一服务间(S2S)鉴权的出站侧能力。
//
// ServiceTokenProvider 用 client_id/secret 向 eiam /oauth/v2/token 换取
// client_credentials 模式的 access token,按租户缓存并提前刷新。
// 依赖 eiam 侧 client_credentials grant(见 docs/designs/service-to-service-auth/design.md)。
// 换取失败一律返回 error,由上层 fail-closed 拒绝业务。
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotomicro/ego/core/elog"
)

// TokenProvider 服务令牌换取器(出站端口)。
type TokenProvider interface {
	// TokenFor 换取(或从缓存返回)access token。
	// tenantID > 0 表示"代表目标租户"(eiam 校验 allowed_tenants 后写入 claim);
	// tenantID == 0 表示"纯服务身份 token"(无租户,用于租户显式传参的端点,如 e-cmdb /internal)。
	// 失败 fail-closed 返回 error。
	TokenFor(ctx context.Context, tenantID int64) (string, error)
}

// tokenResp eiam OAuth2 token 端点响应(仅取所需字段)。
type tokenResp struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

// cachedToken 缓存的 token 及其过期时间。
type cachedToken struct {
	token  string
	expiry time.Time
}

// tokenRefreshSkew 缓存提前刷新量,避免在边界上拿到已过期的 token。
const tokenRefreshSkew = 60 * time.Second

// ServiceTokenProvider 用 client_id/secret 换取 S2S access token,
// 按租户缓存 + 提前刷新,降低 token 端点压力与泄露窗口。
type ServiceTokenProvider struct {
	tokenURL     string
	clientID     string
	clientSecret string
	client       *http.Client
	logger       *elog.Component

	mu    sync.Mutex
	cache map[int64]cachedToken
}

// NewServiceTokenProvider 构造服务令牌换取器。
// 参数留空不报错(换取期 fail-closed),避免装配期因配置缺失阻断服务启动。
func NewServiceTokenProvider(tokenURL, clientID, clientSecret string, logger *elog.Component) *ServiceTokenProvider {
	if logger == nil {
		logger = elog.DefaultLogger
	}
	return &ServiceTokenProvider{
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		client:       &http.Client{Timeout: 10 * time.Second},
		logger:       logger,
		cache:        make(map[int64]cachedToken),
	}
}

// TokenFor 返回缓存中的有效 token,或换取新 token 并缓存。
func (p *ServiceTokenProvider) TokenFor(ctx context.Context, tenantID int64) (string, error) {
	if tenantID < 0 {
		return "", fmt.Errorf("非法租户 ID: %d", tenantID)
	}

	p.mu.Lock()
	if c, ok := p.cache[tenantID]; ok && time.Now().Before(c.expiry) {
		tok := c.token
		p.mu.Unlock()
		return tok, nil
	}
	p.mu.Unlock()

	token, ttl, err := p.exchange(ctx, tenantID)
	if err != nil {
		return "", err
	}

	expiry := time.Now().Add(ttl - tokenRefreshSkew)
	if !expiry.After(time.Now()) {
		expiry = time.Now().Add(ttl / 2) // ttl 过短时退化为半衰期刷新
	}

	p.mu.Lock()
	p.cache[tenantID] = cachedToken{token: token, expiry: expiry}
	p.mu.Unlock()

	return token, nil
}

// exchange 调 eiam token 端点换取 client_credentials token。
func (p *ServiceTokenProvider) exchange(ctx context.Context, tenantID int64) (token string, ttl time.Duration, err error) {
	if p.tokenURL == "" {
		return "", 0, fmt.Errorf("eiam.token_url 未配置,无法换取服务令牌")
	}
	if p.clientID == "" || p.clientSecret == "" {
		return "", 0, fmt.Errorf("eiam.client_id/client_secret 未配置,无法换取服务令牌")
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", p.clientID)
	form.Set("client_secret", p.clientSecret)
	// tenant_id=0 表示纯服务身份(eiam 侧跳过租户授权范围校验,签发无租户 token)
	form.Set("tenant_id", strconv.FormatInt(tenantID, 10))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("构造 token 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("调用 eiam token 端点失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("eiam token 端点返回非 200: %d", resp.StatusCode)
	}

	var body tokenResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", 0, fmt.Errorf("解析 token 响应失败: %w", err)
	}
	if body.AccessToken == "" {
		return "", 0, fmt.Errorf("token 响应缺少 access_token")
	}

	ttl = time.Duration(body.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 15 * time.Minute // 兜底:服务 token 短 TTL
	}
	return body.AccessToken, ttl, nil
}

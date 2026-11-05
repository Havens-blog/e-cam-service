// Package web 告警通知 HTTP 处理器
package web

import (
	"net/url"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
)

// secretQueryKeys 回调地址中需要脱敏的查询参数名（小写比较）。
// 企业微信/钉钉/飞书等机器人的凭据常以这些 key 出现在 URL 查询串里，
// 与云账号 access_key_secret 属于同一类凭据，列表接口不应明文返回。
var secretQueryKeys = map[string]struct{}{
	"key":          {},
	"token":        {},
	"secret":       {},
	"signature":    {},
	"sign":         {},
	"access_token": {},
	"api_key":      {},
	"apikey":       {},
	"password":     {},
}

// maskSecretURL 脱敏回调 URL 中的敏感查询参数：命中 secretQueryKeys 的参数值替换为 ***。
// 无法按 URL 解析（非合法 URL）时原样返回，避免误伤非 URL 字符串。
func maskSecretURL(raw string) string {
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.RawQuery == "" {
		return raw
	}
	q := u.Query()
	changed := false
	for k := range q {
		if _, sensitive := secretQueryKeys[strings.ToLower(k)]; sensitive {
			q.Set(k, "***")
			changed = true
		}
	}
	if !changed {
		return raw
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// maskChannelConfig 对渠道配置中的回调凭据脱敏。
// 只改 string 类型的 webhook / secret 字段；其余字段（如邮箱 host）不受影响。
func maskChannelConfig(ch *domain.NotificationChannel) {
	if ch == nil || ch.Config == nil {
		return
	}
	for _, key := range []string{"webhook", "secret"} {
		if v, ok := ch.Config[key].(string); ok && v != "" {
			ch.Config[key] = maskSecretURL(v)
		}
	}
}

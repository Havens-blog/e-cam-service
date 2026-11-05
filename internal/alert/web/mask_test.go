package web

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
)

func TestMaskSecretURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "企业微信 webhook key 脱敏",
			in:   "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=e03e29e3-949c-4ecc-a292-40c5afe68b7c",
			want: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=%2A%2A%2A",
		},
		{
			name: "钉钉 access_token 脱敏",
			in:   "https://oapi.dingtalk.com/robot/send?access_token=abc123",
			want: "https://oapi.dingtalk.com/robot/send?access_token=%2A%2A%2A",
		},
		{
			name: "大小写不敏感 token",
			in:   "https://example.com/hook?Token=SECRET&foo=bar",
			want: "https://example.com/hook?Token=%2A%2A%2A&foo=bar",
		},
		{
			name: "无敏感参数原样返回",
			in:   "https://example.com/hook?foo=bar&baz=1",
			want: "https://example.com/hook?foo=bar&baz=1",
		},
		{
			name: "空字符串",
			in:   "",
			want: "",
		},
		{
			name: "非 URL 字符串原样返回",
			in:   "not a url",
			want: "not a url",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := maskSecretURL(c.in)
			if got != c.want {
				t.Fatalf("maskSecretURL(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestMaskChannelConfig(t *testing.T) {
	ch := &domain.NotificationChannel{
		Config: map[string]any{
			"webhook": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=secret-key",
			"secret":  "s3cret",
			"foo":     "bar",
		},
	}
	maskChannelConfig(ch)
	if ch.Config["webhook"] == "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=secret-key" {
		t.Fatal("webhook 未脱敏")
	}
	// secret 是纯字符串非 URL，不解析、原样保留（脱敏范围只针对 URL 回调凭据）
	if ch.Config["secret"] != "s3cret" {
		t.Fatalf("secret 被误改: %v", ch.Config["secret"])
	}
	if ch.Config["foo"] != "bar" {
		t.Fatalf("foo 被误改: %v", ch.Config["foo"])
	}
}

func TestMaskChannelConfig_Nil(t *testing.T) {
	maskChannelConfig(nil)
	var ch *domain.NotificationChannel
	maskChannelConfig(ch)
	ch2 := &domain.NotificationChannel{Config: nil}
	maskChannelConfig(ch2)
	// 不 panic 即通过
}

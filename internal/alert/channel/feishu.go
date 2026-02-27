package channel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
)

// FeishuSender 飞书机器人发送器
type FeishuSender struct {
	webhook string
	secret  string
}

func NewFeishuSender(webhook, secret string) *FeishuSender {
	return &FeishuSender{webhook: webhook, secret: secret}
}

func (s *FeishuSender) Type() domain.ChannelType {
	return domain.ChannelFeishu
}

func (s *FeishuSender) Send(ctx context.Context, msg *Message) error {
	body := map[string]any{
		"msg_type": "interactive",
		"card": map[string]any{
			"header": map[string]any{
				"title": map[string]any{
					"tag":     "plain_text",
					"content": msg.Title,
				},
				"template": s.severityColor(msg.Severity),
			},
			"elements": []map[string]any{
				{
					"tag":     "markdown",
					"content": msg.Content,
				},
			},
		},
	}

	if s.secret != "" {
		timestamp := time.Now().Unix()
		sign := s.genSign(timestamp)
		body["timestamp"] = fmt.Sprintf("%d", timestamp)
		body["sign"] = sign
	}

	return postJSON(ctx, s.webhook, body)
}

func (s *FeishuSender) genSign(timestamp int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, s.secret)
	h := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func (s *FeishuSender) severityColor(severity domain.Severity) string {
	switch severity {
	case domain.SeverityCritical:
		return "red"
	case domain.SeverityWarning:
		return "orange"
	default:
		return "blue"
	}
}

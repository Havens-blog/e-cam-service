package channel

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
)

// WeComSender 企业微信机器人发送器
type WeComSender struct {
	webhook string
}

func NewWeComSender(webhook string) *WeComSender {
	return &WeComSender{webhook: webhook}
}

func (s *WeComSender) Type() domain.ChannelType {
	return domain.ChannelWeCom
}

func (s *WeComSender) Send(ctx context.Context, msg *Message) error {
	var body map[string]any
	if msg.Markdown {
		body = map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]any{
				"content": fmt.Sprintf("## %s\n%s", msg.Title, msg.Content),
			},
		}
	} else {
		body = map[string]any{
			"msgtype": "text",
			"text": map[string]any{
				"content": fmt.Sprintf("[%s] %s\n%s", msg.Severity, msg.Title, msg.Content),
			},
		}
	}

	return postJSON(ctx, s.webhook, body)
}

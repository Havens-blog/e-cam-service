// Package channel 通知渠道实现
package channel

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
)

// Message 通知消息
type Message struct {
	Title    string
	Content  string
	Severity domain.Severity
	Markdown bool
}

// Sender 通知发送接口
type Sender interface {
	Send(ctx context.Context, msg *Message) error
	Type() domain.ChannelType
}

// NewSender 根据渠道配置创建发送器
func NewSender(ch domain.NotificationChannel) (Sender, error) {
	switch ch.Type {
	case domain.ChannelDingTalk:
		webhook, _ := ch.Config["webhook"].(string)
		secret, _ := ch.Config["secret"].(string)
		if webhook == "" {
			return nil, fmt.Errorf("dingtalk webhook is required")
		}
		return NewDingTalkSender(webhook, secret), nil
	case domain.ChannelWeCom:
		webhook, _ := ch.Config["webhook"].(string)
		if webhook == "" {
			return nil, fmt.Errorf("wecom webhook is required")
		}
		return NewWeComSender(webhook), nil
	case domain.ChannelFeishu:
		webhook, _ := ch.Config["webhook"].(string)
		secret, _ := ch.Config["secret"].(string)
		if webhook == "" {
			return nil, fmt.Errorf("feishu webhook is required")
		}
		return NewFeishuSender(webhook, secret), nil
	case domain.ChannelEmail:
		host, _ := ch.Config["smtp_host"].(string)
		portF, _ := ch.Config["smtp_port"].(float64)
		user, _ := ch.Config["smtp_user"].(string)
		pass, _ := ch.Config["smtp_pass"].(string)
		from, _ := ch.Config["from"].(string)
		toList, _ := ch.Config["to"].([]any)
		var to []string
		for _, t := range toList {
			if s, ok := t.(string); ok {
				to = append(to, s)
			}
		}
		if host == "" || len(to) == 0 {
			return nil, fmt.Errorf("email smtp_host and to are required")
		}
		return NewEmailSender(host, int(portF), user, pass, from, to), nil
	default:
		return nil, fmt.Errorf("unsupported channel type: %s", ch.Type)
	}
}

// Dispatcher 渠道分发器
type Dispatcher struct {
	senders []Sender
}

// NewDispatcher 创建分发器
func NewDispatcher(channels []domain.NotificationChannel) *Dispatcher {
	d := &Dispatcher{}
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		sender, err := NewSender(ch)
		if err != nil {
			continue
		}
		d.senders = append(d.senders, sender)
	}
	return d
}

// Dispatch 分发消息到所有渠道
func (d *Dispatcher) Dispatch(ctx context.Context, msg *Message) error {
	var lastErr error
	for _, sender := range d.senders {
		if err := sender.Send(ctx, msg); err != nil {
			lastErr = fmt.Errorf("send to %s failed: %w", sender.Type(), err)
		}
	}
	return lastErr
}

package channel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
)

// DingTalkSender 钉钉机器人发送器
type DingTalkSender struct {
	webhook string
	secret  string
}

func NewDingTalkSender(webhook, secret string) *DingTalkSender {
	return &DingTalkSender{webhook: webhook, secret: secret}
}

func (s *DingTalkSender) Type() domain.ChannelType {
	return domain.ChannelDingTalk
}

func (s *DingTalkSender) Send(ctx context.Context, msg *Message) error {
	webhookURL := s.webhook
	if s.secret != "" {
		webhookURL = s.signURL(webhookURL)
	}

	var body map[string]any
	if msg.Markdown {
		body = map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]any{
				"title": msg.Title,
				"text":  msg.Content,
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

	return postJSON(ctx, webhookURL, body)
}

func (s *DingTalkSender) signURL(webhook string) string {
	timestamp := time.Now().UnixMilli()
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, s.secret)

	h := hmac.New(sha256.New, []byte(s.secret))
	h.Write([]byte(stringToSign))
	sign := url.QueryEscape(base64.StdEncoding.EncodeToString(h.Sum(nil)))

	return fmt.Sprintf("%s&timestamp=%d&sign=%s", webhook, timestamp, sign)
}

// postJSON 发送 JSON POST 请求
func postJSON(ctx context.Context, url string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

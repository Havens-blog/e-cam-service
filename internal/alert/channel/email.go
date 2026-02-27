package channel

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
)

// EmailSender 邮件发送器
type EmailSender struct {
	host string
	port int
	user string
	pass string
	from string
	to   []string
}

func NewEmailSender(host string, port int, user, pass, from string, to []string) *EmailSender {
	if from == "" {
		from = user
	}
	return &EmailSender{host: host, port: port, user: user, pass: pass, from: from, to: to}
}

func (s *EmailSender) Type() domain.ChannelType {
	return domain.ChannelEmail
}

func (s *EmailSender) Send(_ context.Context, msg *Message) error {
	subject := fmt.Sprintf("[%s] %s", msg.Severity, msg.Title)

	contentType := "text/plain"
	if msg.Markdown {
		contentType = "text/html"
	}

	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: %s; charset=UTF-8\r\n\r\n%s",
		s.from,
		strings.Join(s.to, ","),
		subject,
		contentType,
		msg.Content,
	)

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	var auth smtp.Auth
	if s.user != "" && s.pass != "" {
		auth = smtp.PlainAuth("", s.user, s.pass, s.host)
	}

	return smtp.SendMail(addr, auth, s.from, s.to, []byte(body))
}

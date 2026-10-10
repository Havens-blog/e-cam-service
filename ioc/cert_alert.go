package ioc

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/alert/channel"
	alertdomain "github.com/Havens-blog/e-cam-service/internal/alert/domain"
	alertdao "github.com/Havens-blog/e-cam-service/internal/alert/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cert/alertpub"
	"github.com/spf13/viper"
)

// 本文件为 cert 告警端口的 alert 侧适配（cert 服务抽取 · 绞杀者 step 1）：
// internal/cert/alertpub 对 internal/alert 零 import，所需的通用基建（SMTP 邮件
// 发送、投递记录持久化）由此组合根用 alert 既有能力适配到 cert 端口。adapter 承载
// alert 枚举与 cert 中性类型的互转，使两域互不反向依赖。

// certSMTPConfig 证书域邮件通道 SMTP 凭据（应用 config 键 alert.cert_smtp）。
type certSMTPConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
	User string `mapstructure:"user"`
	Pass string `mapstructure:"pass"`
	From string `mapstructure:"from"`
}

const defaultCertSMTPPort = 25

// loadCertSMTPConfig 读取 alert.cert_smtp；键缺省时返回零值（邮件通道停用）。
func loadCertSMTPConfig() certSMTPConfig {
	var cfg certSMTPConfig
	if err := viper.UnmarshalKey("alert.cert_smtp", &cfg); err != nil {
		return certSMTPConfig{}
	}
	if cfg.Port == 0 && cfg.Host != "" {
		cfg.Port = defaultCertSMTPPort
	}
	return cfg
}

// certEmailSinkAdapter 把 cert alertpub.EmailSink 适配到 alert 通用 EmailSender。
// 按接收人动态构造 sender（与原 certEmailChannel 行为一致）。
type certEmailSinkAdapter struct {
	cfg certSMTPConfig
}

func (a certEmailSinkAdapter) SendCertEmail(ctx context.Context, title, body, severity string, to []string) error {
	sender := channel.NewEmailSender(a.cfg.Host, a.cfg.Port, a.cfg.User, a.cfg.Pass, a.cfg.From, to)
	return sender.Send(ctx, &channel.Message{
		Title:    title,
		Content:  body,
		Severity: alertdomain.Severity(severity),
	})
}

// certDeliveryRecorderAdapter 把 cert alertpub.DeliveryRecorder 适配到 alert
// AlertDAO.CreateEvent（终态直写 ecam_alert_event；归属推迟到接口背后）。
type certDeliveryRecorderAdapter struct {
	dao alertdao.AlertDAO
}

func (a certDeliveryRecorderAdapter) Record(ctx context.Context, rec alertpub.DeliveryRecord) error {
	if a.dao == nil {
		return nil
	}
	evt := alertdomain.AlertEvent{
		Type:     alertdomain.AlertType(rec.Type),
		Severity: alertdomain.Severity(rec.Severity),
		Title:    rec.Title,
		Content:  rec.Content,
		Source:   rec.Source,
		Status:   alertdomain.EventStatus(rec.Status),
	}
	if rec.SentAt != nil {
		t := time.UnixMilli(*rec.SentAt)
		evt.SentAt = &t
	}
	_, err := a.dao.CreateEvent(ctx, evt)
	return err
}

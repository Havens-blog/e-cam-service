package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// ---------------------------------------------------------------------
// 全局配置服务（任务 4.5：settings CRUD / exemptions / test 告警；
// crds 端点直接复用 3.4 CrdRegistrationService，本服务不承载）
// ---------------------------------------------------------------------

// SettingsView 全局配置视图（GET /settings 响应载荷：告警配置+阈值+豁免清单）。
// Hard Rule：不含任何私钥/凭证字段（webhookUrls 为配置可见项，SMTP 密码等
// 敏感凭据不返回——本模型本身不承载此类字段）。
type SettingsView struct {
	WebhookURLs            []string
	EmailGroup             []string
	ChannelConfirmed       bool
	Thresholds             domain.Thresholds
	VerifyWindowRoute      *domain.VerifyWindowRoute
	WildcardProbeOverrides map[string]string
	Exemptions             []domain.Exemption
}

// UpdateSettingsInput PUT /settings 载荷（全量替换五个字段组；
// thresholds 越界整体拒绝，不做部分写入）。
type UpdateSettingsInput struct {
	WebhookURLs            []string
	EmailGroup             []string
	VerifyWindowRoute      *domain.VerifyWindowRoute
	WildcardProbeOverrides map[string]string
	Thresholds             domain.Thresholds
}

// ExemptionInput 豁免添加入参。
type ExemptionInput struct {
	Domain   string
	Reason   string
	Operator string
}

// TestAlertResult POST /settings/test 结果（返回成功/失败原因）。
type TestAlertResult struct {
	Sent   bool
	Reason string
}

// SettingsService 全局配置读写（运维主管/审计角色面）。
type SettingsService interface {
	// GetSettings 告警配置+阈值+豁免清单（文档不存在时返回 DEFAULT 填充视图）。
	GetSettings(ctx context.Context) (SettingsView, error)
	// UpdateSettings 更新告警接收/通配符 override/阈值。先整体校验 thresholds
	// 界值（*domain.ThresholdsInvalidError → 400，Hard Rule：越界整体拒绝，
	// 不做部分写入），通过后全量保存五个字段组并返回新视图。
	UpdateSettings(ctx context.Context, in UpdateSettingsInput) (SettingsView, error)
	// AddExemption 添加/更新豁免（domain 唯一，Upsert 语义；operator 记审计主体）。
	AddExemption(ctx context.Context, in ExemptionInput) (domain.Exemption, error)
	// RemoveExemption 移除豁免。
	RemoveExemption(ctx context.Context, domainName string) error
	// SendTestAlert 经 4.3 通道发送测试告警；成功后将 channelConfirmed 置 true
	//（渠道确认为 PRD 前置依赖，测试告警触达即确认）。失败返回 Sent=false+原因。
	SendTestAlert(ctx context.Context, operator string) (TestAlertResult, error)
}

type settingsService struct {
	alertCfg  domain.AlertConfigRepository
	exempts   domain.ExemptionRepository
	publisher CertAlertPublisher // nil 时回退日志实现
}

// NewSettingsService 创建全局配置服务。publisher 为 4.3 通道注入缝
// （nil 回退日志发布器，通道落地前的默认路径）。
func NewSettingsService(
	alertCfg domain.AlertConfigRepository,
	exempts domain.ExemptionRepository,
	publisher CertAlertPublisher,
) SettingsService {
	if publisher == nil {
		publisher = NewLoggingAlertPublisher()
	}
	return &settingsService{alertCfg: alertCfg, exempts: exempts, publisher: publisher}
}

// GetSettings 读取配置与豁免清单（空集归一为空切片/空 map，稳定序列化）。
func (s *settingsService) GetSettings(ctx context.Context) (SettingsView, error) {
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return SettingsView{}, fmt.Errorf("settings: get alert config: %w", err)
	}
	exemptions, err := s.exempts.List(ctx)
	if err != nil {
		return SettingsView{}, fmt.Errorf("settings: list exemptions: %w", err)
	}
	return toSettingsView(cfg, exemptions), nil
}

// UpdateSettings 阈值界值服务端校验（Hard Rule）→ 全量替换五字段组 → 保存。
func (s *settingsService) UpdateSettings(ctx context.Context, in UpdateSettingsInput) (SettingsView, error) {
	if err := domain.ValidateThresholds(in.Thresholds); err != nil {
		return SettingsView{}, err // *ThresholdsInvalidError → 400，整体拒绝不写入
	}
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return SettingsView{}, fmt.Errorf("settings: get alert config: %w", err)
	}
	cfg.WebhookURLs = in.WebhookURLs
	cfg.EmailGroup = in.EmailGroup
	cfg.VerifyWindowRoute = in.VerifyWindowRoute
	cfg.WildcardProbeOverrides = in.WildcardProbeOverrides
	cfg.Thresholds = in.Thresholds
	// channelConfirmed 不经 PUT 改写：仅由测试告警成功确认（PRD 渠道确认门控）
	if err := s.alertCfg.Save(ctx, &cfg); err != nil {
		return SettingsView{}, fmt.Errorf("settings: save alert config: %w", err)
	}
	exemptions, err := s.exempts.List(ctx)
	if err != nil {
		return SettingsView{}, fmt.Errorf("settings: list exemptions: %w", err)
	}
	return toSettingsView(cfg, exemptions), nil
}

// AddExemption 豁免 Upsert（domain 唯一；重写保留原 createdAt 语义由仓储承接）。
func (s *settingsService) AddExemption(ctx context.Context, in ExemptionInput) (domain.Exemption, error) {
	domainName := strings.TrimSpace(in.Domain)
	if domainName == "" {
		return domain.Exemption{}, fmt.Errorf("cert: exemption domain is required")
	}
	e := &domain.Exemption{
		Domain:   domainName,
		Reason:   strings.TrimSpace(in.Reason),
		Operator: strings.TrimSpace(in.Operator),
	}
	if err := s.exempts.Upsert(ctx, e); err != nil {
		return domain.Exemption{}, fmt.Errorf("settings: upsert exemption: %w", err)
	}
	return *e, nil
}

// RemoveExemption 移除豁免（domain trim；幂等——未命中亦成功）。
func (s *settingsService) RemoveExemption(ctx context.Context, domainName string) error {
	if err := s.exempts.DeleteByDomain(ctx, strings.TrimSpace(domainName)); err != nil {
		return fmt.Errorf("settings: delete exemption: %w", err)
	}
	return nil
}

// SendTestAlert 渠道连通性验证：无接收人配置直接失败；发布成功即确认渠道
// （channelConfirmed=true 持久化）。测试告警 category=test，不计入四类业务告警。
func (s *settingsService) SendTestAlert(ctx context.Context, operator string) (TestAlertResult, error) {
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return TestAlertResult{}, fmt.Errorf("settings: get alert config: %w", err)
	}
	if len(cfg.WebhookURLs) == 0 && len(cfg.EmailGroup) == 0 {
		return TestAlertResult{Sent: false, Reason: "no alert receivers configured"}, nil
	}
	event := CertAlertEvent{
		Category: AlertCategoryTest,
		Title:    "证书告警渠道测试",
		Detail:   fmt.Sprintf("test alert via POST /api/v1/certs/settings/test by %s", operator),
		At:       time.Now(),
	}
	if err := s.publisher.PublishAlert(ctx, event); err != nil {
		return TestAlertResult{Sent: false, Reason: err.Error()}, nil
	}
	if !cfg.ChannelConfirmed {
		cfg.ChannelConfirmed = true
		if err := s.alertCfg.Save(ctx, &cfg); err != nil {
			return TestAlertResult{}, fmt.Errorf("settings: save alert config: %w", err)
		}
	}
	return TestAlertResult{Sent: true}, nil
}

// toSettingsView 配置+豁免 → 视图（空集归一，避免 null 序列化）。
func toSettingsView(cfg domain.AlertConfig, exemptions []domain.Exemption) SettingsView {
	if exemptions == nil {
		exemptions = []domain.Exemption{}
	}
	overrides := make(map[string]string, len(cfg.WildcardProbeOverrides))
	for k, v := range cfg.WildcardProbeOverrides {
		overrides[k] = v
	}
	view := SettingsView{
		WebhookURLs:            append([]string(nil), cfg.WebhookURLs...),
		EmailGroup:             append([]string(nil), cfg.EmailGroup...),
		ChannelConfirmed:       cfg.ChannelConfirmed,
		Thresholds:             cfg.Thresholds,
		WildcardProbeOverrides: overrides,
		Exemptions:             exemptions,
	}
	if view.WebhookURLs == nil {
		view.WebhookURLs = []string{}
	}
	if view.EmailGroup == nil {
		view.EmailGroup = []string{}
	}
	if view.Thresholds.ExpiryLevels == nil {
		view.Thresholds.ExpiryLevels = []int{}
	}
	if cfg.VerifyWindowRoute != nil {
		vwr := *cfg.VerifyWindowRoute
		vwr.WebhookURLs = append([]string(nil), vwr.WebhookURLs...)
		vwr.EmailGroup = append([]string(nil), vwr.EmailGroup...)
		if vwr.WebhookURLs == nil {
			vwr.WebhookURLs = []string{}
		}
		if vwr.EmailGroup == nil {
			vwr.EmailGroup = []string{}
		}
		view.VerifyWindowRoute = &vwr
	}
	return view
}

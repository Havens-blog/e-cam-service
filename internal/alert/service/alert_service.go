// Package service 告警通知业务逻辑
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/alert/channel"
	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
	"github.com/Havens-blog/e-cam-service/internal/alert/repository/dao"
	"github.com/gotomicro/ego/core/elog"
)

// AlertService 告警服务
type AlertService struct {
	dao    dao.AlertDAO
	logger *elog.Component
}

// NewAlertService 创建告警服务
func NewAlertService(dao dao.AlertDAO, logger *elog.Component) *AlertService {
	return &AlertService{dao: dao, logger: logger}
}

// ========== 告警规则管理 ==========

func (s *AlertService) CreateRule(ctx context.Context, rule domain.AlertRule) (int64, error) {
	if rule.Name == "" {
		return 0, fmt.Errorf("规则名称不能为空")
	}
	if rule.Type == "" {
		return 0, fmt.Errorf("规则类型不能为空")
	}
	rule.Enabled = true
	return s.dao.CreateRule(ctx, rule)
}

func (s *AlertService) UpdateRule(ctx context.Context, rule domain.AlertRule) error {
	return s.dao.UpdateRule(ctx, rule)
}

func (s *AlertService) GetRule(ctx context.Context, id int64) (domain.AlertRule, error) {
	return s.dao.GetRuleByID(ctx, id)
}

func (s *AlertService) ListRules(ctx context.Context, filter domain.AlertRuleFilter) ([]domain.AlertRule, int64, error) {
	return s.dao.ListRules(ctx, filter)
}

func (s *AlertService) DeleteRule(ctx context.Context, id int64) error {
	return s.dao.DeleteRule(ctx, id)
}

func (s *AlertService) ToggleRule(ctx context.Context, id int64, enabled bool) error {
	rule, err := s.dao.GetRuleByID(ctx, id)
	if err != nil {
		return err
	}
	rule.Enabled = enabled
	return s.dao.UpdateRule(ctx, rule)
}

// ========== 告警事件 ==========

func (s *AlertService) ListEvents(ctx context.Context, filter domain.AlertEventFilter) ([]domain.AlertEvent, int64, error) {
	return s.dao.ListEvents(ctx, filter)
}

// EmitEvent 触发告警事件 - 匹配规则并创建事件
func (s *AlertService) EmitEvent(ctx context.Context, event domain.AlertEvent) error {
	// 查找匹配的启用规则
	enabled := true
	rules, _, err := s.dao.ListRules(ctx, domain.AlertRuleFilter{
		TenantID: event.TenantID,
		Type:     event.Type,
		Enabled:  &enabled,
	})
	if err != nil {
		return fmt.Errorf("查询告警规则失败: %w", err)
	}

	if len(rules) == 0 {
		s.logger.Debug("无匹配的告警规则",
			elog.String("type", string(event.Type)),
			elog.String("tenant_id", event.TenantID))
		return nil
	}

	// 对每个匹配的规则创建事件
	for _, rule := range rules {
		if !s.matchRule(rule, event) {
			continue
		}

		evt := event
		evt.RuleID = rule.ID
		if _, err := s.dao.CreateEvent(ctx, evt); err != nil {
			s.logger.Error("创建告警事件失败",
				elog.Int64("rule_id", rule.ID),
				elog.FieldErr(err))
			continue
		}

		s.logger.Info("告警事件已创建",
			elog.String("title", evt.Title),
			elog.String("severity", string(evt.Severity)),
			elog.Int64("rule_id", rule.ID))
	}

	return nil
}

// matchRule 检查事件是否匹配规则的过滤条件
func (s *AlertService) matchRule(rule domain.AlertRule, event domain.AlertEvent) bool {
	// 检查账号过滤
	if len(rule.AccountIDs) > 0 {
		accountID, _ := event.Content["account_id"].(float64)
		if !containsInt64(rule.AccountIDs, int64(accountID)) {
			return false
		}
	}

	// 检查资源类型过滤
	if len(rule.ResourceTypes) > 0 {
		resourceType, _ := event.Content["resource_type"].(string)
		if !containsString(rule.ResourceTypes, resourceType) {
			return false
		}
	}

	// 检查地域过滤
	if len(rule.Regions) > 0 {
		region, _ := event.Content["region"].(string)
		if !containsString(rule.Regions, region) {
			return false
		}
	}

	return true
}

// ProcessPendingEvents 处理待发送的告警事件
func (s *AlertService) ProcessPendingEvents(ctx context.Context) error {
	events, err := s.dao.GetPendingEvents(ctx, 50)
	if err != nil {
		return fmt.Errorf("获取待处理事件失败: %w", err)
	}

	for _, event := range events {
		if err := s.sendEvent(ctx, event); err != nil {
			s.logger.Error("发送告警事件失败",
				elog.Int64("event_id", event.ID),
				elog.FieldErr(err))
			s.dao.IncrementRetry(ctx, event.ID)
			if event.RetryCount >= 2 {
				s.dao.UpdateEventStatus(ctx, event.ID, domain.EventStatusFailed)
			}
			continue
		}
		s.dao.UpdateEventStatus(ctx, event.ID, domain.EventStatusSent)
	}

	return nil
}

// sendEvent 发送单个告警事件
func (s *AlertService) sendEvent(ctx context.Context, event domain.AlertEvent) error {
	rule, err := s.dao.GetRuleByID(ctx, event.RuleID)
	if err != nil {
		return fmt.Errorf("获取规则失败: %w", err)
	}

	channels, err := s.dao.GetChannelsByIDs(ctx, rule.ChannelIDs)
	if err != nil {
		return fmt.Errorf("获取通知渠道失败: %w", err)
	}

	if len(channels) == 0 {
		s.logger.Warn("规则无可用通知渠道", elog.Int64("rule_id", rule.ID))
		return nil
	}

	msg := s.buildMessage(event)
	dispatcher := channel.NewDispatcher(channels)
	return dispatcher.Dispatch(ctx, msg)
}

// buildMessage 构建通知消息
func (s *AlertService) buildMessage(event domain.AlertEvent) *channel.Message {
	var content strings.Builder

	switch event.Type {
	case domain.AlertTypeResourceChange:
		s.buildResourceChangeContent(&content, event)
	case domain.AlertTypeSyncFailure:
		s.buildSyncFailureContent(&content, event)
	case domain.AlertTypeExpiration:
		s.buildExpirationContent(&content, event)
	case domain.AlertTypeSecurityGroup:
		s.buildSecurityGroupContent(&content, event)
	default:
		content.WriteString(fmt.Sprintf("%v", event.Content))
	}

	return &channel.Message{
		Title:    event.Title,
		Content:  content.String(),
		Severity: event.Severity,
		Markdown: true,
	}
}

func (s *AlertService) buildResourceChangeContent(b *strings.Builder, event domain.AlertEvent) {
	changeType, _ := event.Content["change_type"].(string)
	resourceType, _ := event.Content["resource_type"].(string)
	assetID, _ := event.Content["asset_id"].(string)
	assetName, _ := event.Content["asset_name"].(string)
	provider, _ := event.Content["provider"].(string)
	region, _ := event.Content["region"].(string)

	b.WriteString(fmt.Sprintf("**变更类型**: %s\n", changeType))
	b.WriteString(fmt.Sprintf("**资源类型**: %s\n", resourceType))
	b.WriteString(fmt.Sprintf("**资源ID**: %s\n", assetID))
	if assetName != "" {
		b.WriteString(fmt.Sprintf("**资源名称**: %s\n", assetName))
	}
	b.WriteString(fmt.Sprintf("**云厂商**: %s\n", provider))
	b.WriteString(fmt.Sprintf("**地域**: %s\n", region))
	b.WriteString(fmt.Sprintf("**时间**: %s\n", time.Now().Format("2006-01-02 15:04:05")))
}

func (s *AlertService) buildSyncFailureContent(b *strings.Builder, event domain.AlertEvent) {
	taskID, _ := event.Content["task_id"].(string)
	accountName, _ := event.Content["account_name"].(string)
	reason, _ := event.Content["reason"].(string)

	b.WriteString(fmt.Sprintf("**任务ID**: %s\n", taskID))
	b.WriteString(fmt.Sprintf("**云账号**: %s\n", accountName))
	b.WriteString(fmt.Sprintf("**失败原因**: %s\n", reason))
	b.WriteString(fmt.Sprintf("**时间**: %s\n", time.Now().Format("2006-01-02 15:04:05")))
}

func (s *AlertService) buildExpirationContent(b *strings.Builder, event domain.AlertEvent) {
	resourceType, _ := event.Content["resource_type"].(string)
	assetID, _ := event.Content["asset_id"].(string)
	assetName, _ := event.Content["asset_name"].(string)
	expireTime, _ := event.Content["expire_time"].(string)
	daysLeft, _ := event.Content["days_left"].(float64)

	b.WriteString(fmt.Sprintf("**资源类型**: %s\n", resourceType))
	b.WriteString(fmt.Sprintf("**资源ID**: %s\n", assetID))
	if assetName != "" {
		b.WriteString(fmt.Sprintf("**资源名称**: %s\n", assetName))
	}
	b.WriteString(fmt.Sprintf("**到期时间**: %s\n", expireTime))
	b.WriteString(fmt.Sprintf("**剩余天数**: %.0f 天\n", daysLeft))
}

func (s *AlertService) buildSecurityGroupContent(b *strings.Builder, event domain.AlertEvent) {
	sgID, _ := event.Content["security_group_id"].(string)
	changeType, _ := event.Content["change_type"].(string)
	ruleDetail, _ := event.Content["rule_detail"].(string)

	b.WriteString(fmt.Sprintf("**安全组ID**: %s\n", sgID))
	b.WriteString(fmt.Sprintf("**变更类型**: %s\n", changeType))
	b.WriteString(fmt.Sprintf("**规则详情**: %s\n", ruleDetail))
	b.WriteString(fmt.Sprintf("**时间**: %s\n", time.Now().Format("2006-01-02 15:04:05")))
}

// ========== 通知渠道管理 ==========

func (s *AlertService) CreateChannel(ctx context.Context, ch domain.NotificationChannel) (int64, error) {
	if ch.Name == "" {
		return 0, fmt.Errorf("渠道名称不能为空")
	}
	if ch.Type == "" {
		return 0, fmt.Errorf("渠道类型不能为空")
	}
	ch.Enabled = true
	return s.dao.CreateChannel(ctx, ch)
}

func (s *AlertService) UpdateChannel(ctx context.Context, ch domain.NotificationChannel) error {
	return s.dao.UpdateChannel(ctx, ch)
}

func (s *AlertService) GetChannel(ctx context.Context, id int64) (domain.NotificationChannel, error) {
	return s.dao.GetChannelByID(ctx, id)
}

func (s *AlertService) ListChannels(ctx context.Context, filter domain.ChannelFilter) ([]domain.NotificationChannel, int64, error) {
	return s.dao.ListChannels(ctx, filter)
}

func (s *AlertService) DeleteChannel(ctx context.Context, id int64) error {
	return s.dao.DeleteChannel(ctx, id)
}

// TestChannel 测试通知渠道
func (s *AlertService) TestChannel(ctx context.Context, id int64) error {
	ch, err := s.dao.GetChannelByID(ctx, id)
	if err != nil {
		return fmt.Errorf("获取渠道失败: %w", err)
	}

	sender, err := channel.NewSender(ch)
	if err != nil {
		return fmt.Errorf("创建发送器失败: %w", err)
	}

	msg := &channel.Message{
		Title:    "告警通知测试",
		Content:  "这是一条测试消息，如果您收到此消息，说明通知渠道配置正确。",
		Severity: domain.SeverityInfo,
	}

	return sender.Send(ctx, msg)
}

// ========== 辅助函数 ==========

func containsInt64(slice []int64, val int64) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

func containsString(slice []string, val string) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

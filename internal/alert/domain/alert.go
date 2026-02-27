// Package domain 告警通知领域模型
package domain

import "time"

// AlertType 告警类型
type AlertType string

const (
	AlertTypeResourceChange AlertType = "resource_change" // 资源变更
	AlertTypeSyncFailure    AlertType = "sync_failure"    // 同步失败
	AlertTypeExpiration     AlertType = "expiration"      // 资源过期
	AlertTypeSecurityGroup  AlertType = "security_group"  // 安全组变更
)

// Severity 告警级别
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// EventStatus 告警事件状态
type EventStatus string

const (
	EventStatusPending  EventStatus = "pending"
	EventStatusSent     EventStatus = "sent"
	EventStatusFailed   EventStatus = "failed"
	EventStatusSilenced EventStatus = "silenced"
)

// ChannelType 通知渠道类型
type ChannelType string

const (
	ChannelDingTalk ChannelType = "dingtalk"
	ChannelWeCom    ChannelType = "wecom"
	ChannelFeishu   ChannelType = "feishu"
	ChannelEmail    ChannelType = "email"
)

// AlertRule 告警规则
type AlertRule struct {
	ID               int64          `json:"id" bson:"id"`
	Name             string         `json:"name" bson:"name"`
	Type             AlertType      `json:"type" bson:"type"`
	Condition        map[string]any `json:"condition" bson:"condition"`
	ChannelIDs       []int64        `json:"channel_ids" bson:"channel_ids"`
	AccountIDs       []int64        `json:"account_ids" bson:"account_ids"`
	ResourceTypes    []string       `json:"resource_types" bson:"resource_types"`
	Regions          []string       `json:"regions" bson:"regions"`
	SilenceDuration  int            `json:"silence_duration" bson:"silence_duration"` // 静默期(分钟)
	EscalateAfter    int            `json:"escalate_after" bson:"escalate_after"`     // 连续N次后升级
	EscalateChannels []int64        `json:"escalate_channels" bson:"escalate_channels"`
	TenantID         string         `json:"tenant_id" bson:"tenant_id"`
	Enabled          bool           `json:"enabled" bson:"enabled"`
	CreateTime       time.Time      `json:"create_time" bson:"create_time"`
	UpdateTime       time.Time      `json:"update_time" bson:"update_time"`
}

// AlertEvent 告警事件
type AlertEvent struct {
	ID         int64          `json:"id" bson:"id"`
	RuleID     int64          `json:"rule_id" bson:"rule_id"`
	Type       AlertType      `json:"type" bson:"type"`
	Severity   Severity       `json:"severity" bson:"severity"`
	Title      string         `json:"title" bson:"title"`
	Content    map[string]any `json:"content" bson:"content"`
	Source     string         `json:"source" bson:"source"` // e.g. "sync_task:123"
	TenantID   string         `json:"tenant_id" bson:"tenant_id"`
	Status     EventStatus    `json:"status" bson:"status"`
	RetryCount int            `json:"retry_count" bson:"retry_count"`
	CreateTime time.Time      `json:"create_time" bson:"create_time"`
	SentAt     *time.Time     `json:"sent_at" bson:"sent_at"`
}

// NotificationChannel 通知渠道
type NotificationChannel struct {
	ID         int64          `json:"id" bson:"id"`
	Name       string         `json:"name" bson:"name"`
	Type       ChannelType    `json:"type" bson:"type"`
	Config     map[string]any `json:"config" bson:"config"`
	TenantID   string         `json:"tenant_id" bson:"tenant_id"`
	Enabled    bool           `json:"enabled" bson:"enabled"`
	CreateTime time.Time      `json:"create_time" bson:"create_time"`
	UpdateTime time.Time      `json:"update_time" bson:"update_time"`
}

// ResourceChange 资源变更记录
type ResourceChange struct {
	ChangeType   string         `json:"change_type"`   // added, removed, modified
	ResourceType string         `json:"resource_type"` // ecs, rds, vpc...
	AssetID      string         `json:"asset_id"`
	AssetName    string         `json:"asset_name"`
	AccountID    int64          `json:"account_id"`
	Provider     string         `json:"provider"`
	Region       string         `json:"region"`
	Details      map[string]any `json:"details"` // 变更详情
}

// AlertRuleFilter 告警规则过滤条件
type AlertRuleFilter struct {
	TenantID string
	Type     AlertType
	Enabled  *bool
	Offset   int64
	Limit    int64
}

// AlertEventFilter 告警事件过滤条件
type AlertEventFilter struct {
	TenantID string
	Type     AlertType
	Severity Severity
	Status   EventStatus
	RuleID   int64
	Offset   int64
	Limit    int64
}

// ChannelFilter 通知渠道过滤条件
type ChannelFilter struct {
	TenantID string
	Type     ChannelType
	Enabled  *bool
	Offset   int64
	Limit    int64
}

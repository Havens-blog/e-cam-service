package web

// CreateRuleReq 创建告警规则请求
type CreateRuleReq struct {
	Name             string         `json:"name" binding:"required"`
	Type             string         `json:"type" binding:"required"` // resource_change, sync_failure, expiration, security_group
	Condition        map[string]any `json:"condition"`
	ChannelIDs       []int64        `json:"channel_ids" binding:"required"`
	AccountIDs       []int64        `json:"account_ids"`
	ResourceTypes    []string       `json:"resource_types"`
	Regions          []string       `json:"regions"`
	SilenceDuration  int            `json:"silence_duration"`
	EscalateAfter    int            `json:"escalate_after"`
	EscalateChannels []int64        `json:"escalate_channels"`
}

// ToggleRuleReq 启用/禁用告警规则请求
type ToggleRuleReq struct {
	Enabled bool `json:"enabled"`
}

// CreateChannelReq 创建通知渠道请求
type CreateChannelReq struct {
	Name   string         `json:"name" binding:"required"`
	Type   string         `json:"type" binding:"required"` // dingtalk, wecom, feishu, email
	Config map[string]any `json:"config" binding:"required"`
}

// Result 统一响应
type Result struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

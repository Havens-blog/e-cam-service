package domain

import "time"

// RuleOperator 规则操作符
const (
	OperatorEq       = "eq"       // 等于
	OperatorNe       = "ne"       // 不等于
	OperatorContains = "contains" // 包含
	OperatorRegex    = "regex"    // 正则匹配
	OperatorIn       = "in"       // 在列表中
	OperatorNotIn    = "not_in"   // 不在列表中
	OperatorExists   = "exists"   // 存在
)

// BindingRule 自动绑定规则
type BindingRule struct {
	ID          int64           // 规则ID
	NodeID      int64           // 目标节点ID
	EnvID       int64           // 目标环境ID
	Name        string          // 规则名称
	TenantID    string          // 租户ID
	Priority    int             // 优先级 (数字越小优先级越高)
	Conditions  []RuleCondition // 匹配条件 (AND关系)
	Enabled     bool            // 是否启用
	Description string          // 规则描述
	CreateTime  time.Time
	UpdateTime  time.Time
}

// RuleCondition 规则条件
type RuleCondition struct {
	Field    string `json:"field"`    // 匹配字段 (provider/region/tag.xxx/name/attributes.xxx)
	Operator string `json:"operator"` // 操作符 (eq/ne/contains/regex/in)
	Value    string `json:"value"`    // 匹配值
}

// Validate 验证规则
func (r *BindingRule) Validate() error {
	if r.Name == "" {
		return ErrRuleNameEmpty
	}
	if r.NodeID == 0 {
		return ErrRuleNodeIDEmpty
	}
	if r.TenantID == "" {
		return ErrRuleTenantIDEmpty
	}
	if len(r.Conditions) == 0 {
		return ErrRuleConditionsEmpty
	}
	return nil
}

// RuleFilter 规则过滤条件
type RuleFilter struct {
	TenantID string
	NodeID   int64
	Enabled  *bool
	Name     string
	Offset   int64
	Limit    int64
}

// RuleMatchResult 规则匹配结果
type RuleMatchResult struct {
	RuleID     int64  // 匹配的规则ID
	NodeID     int64  // 目标节点ID
	ResourceID int64  // 资源ID
	Matched    bool   // 是否匹配
	Reason     string // 匹配/不匹配原因
}

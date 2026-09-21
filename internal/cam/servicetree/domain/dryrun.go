package domain

// 规则试运行（dry-run）绑定状态取值
const (
	// BindStatusUnbound 未绑定（或在指定环境下无绑定）
	BindStatusUnbound = "unbound"
)

// MaxDryRunItems 单次 dry-run 返回命中清单上限（超出部分截断，Total 保留真实命中总数）
const MaxDryRunItems = 500

// DryRunRequest 规则试运行请求（临时规则体，不落库）
type DryRunRequest struct {
	// NodeID 目标节点ID（可选，仅上下文信息，不参与匹配）
	NodeID int64
	// EnvID 目标环境ID（可选；提供时「当前绑定状态」按该环境口径判定）
	EnvID int64
	// Conditions 匹配条件（与 BindingRule.Conditions 同构，AND 关系）
	Conditions []RuleCondition
}

// DryRunMatchItem 试运行命中的单个资产（含当前绑定状态，只读标注）
type DryRunMatchItem struct {
	ResourceID int64  `json:"resource_id"`
	AssetID    string `json:"asset_id"`
	AssetName  string `json:"asset_name"`
	Provider   string `json:"provider"`
	Region     string `json:"region"`
	// BindStatus 当前绑定状态：unbound/manual/rule（manual/rule 复用 BindType 取值）
	BindStatus string `json:"bind_status"`
	// BoundNodeID/BoundNodeName/BoundEnvID/BoundRuleID 仅在已绑定时非零
	BoundNodeID   int64  `json:"bound_node_id,omitempty"`
	BoundNodeName string `json:"bound_node_name,omitempty"`
	BoundEnvID    int64  `json:"bound_env_id,omitempty"`
	BoundRuleID   int64  `json:"bound_rule_id,omitempty"`
}

// DryRunResult 试运行结果：items 截断到 MaxDryRunItems，total 为真实命中总数
type DryRunResult struct {
	Items  []DryRunMatchItem `json:"items"`
	Total  int64             `json:"total"`
	Capped bool              `json:"capped"`
}

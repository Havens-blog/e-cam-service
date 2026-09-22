package domain

// RebindCandidate 改绑候选：一条 rule 绑定资产按当前启用规则重算后应改绑到的新目标。
// 只读预览产物。manual 绑定永不进入候选（锁定不动）。
type RebindCandidate struct {
	ResourceID   int64  `json:"resource_id"`
	AssetID      string `json:"asset_id"`
	AssetName    string `json:"asset_name"`
	Provider     string `json:"provider"`
	Region       string `json:"region"`
	FromNodeID   int64  `json:"from_node_id"`
	FromNodeName string `json:"from_node_name,omitempty"`
	FromEnvID    int64  `json:"from_env_id"`
	FromRuleID   int64  `json:"from_rule_id"`
	ToNodeID     int64  `json:"to_node_id"`
	ToNodeName   string `json:"to_node_name,omitempty"`
	ToEnvID      int64  `json:"to_env_id"`
	ToRuleID     int64  `json:"to_rule_id"`
}

// RebindPlan 改绑计划预览结果
type RebindPlan struct {
	Items []RebindCandidate `json:"items"`
	Total int64             `json:"total"`
}

// RebindRequest 改绑确认请求：仅对列出的资源 ID 应用改绑（预览后再确认落库）。
type RebindRequest struct {
	ResourceIDs []int64 `json:"resource_ids"`
}
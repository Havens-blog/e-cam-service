package domain

// AssetSummary 节点子树资产聚合统计（GET /nodes/:id/asset-summary）
// 聚合口径：子树（含自身）全部节点绑定资产，一次 node_id IN 查询后内存归并。
type AssetSummary struct {
	Total         int64            `json:"total"`
	ByEnvironment map[string]int64 `json:"by_environment"` // key: 环境代码 (dev/test/staging/prod)，未知环境为 env_<id>
	ByProvider    map[string]int64 `json:"by_provider"`    // key: 云平台 (provider attribute)
	ByType        map[string]int64 `json:"by_type"`        // key: 资产类型 (model_uid 去厂商前缀)
	ByBindType    map[string]int64 `json:"by_bind_type"`   // key: 绑定来源 (manual/rule)
	Suspicious    []AssetSuspicion `json:"suspicious"`     // 环境疑异清单（只提示不改绑）
}

// AssetSuspicion 环境疑异资产
type AssetSuspicion struct {
	AssetID      string `json:"asset_id"`       // 云厂商资产ID
	AssetName    string `json:"asset_name"`     // 资产名称
	BoundEnvID   int64  `json:"bound_env_id"`   // 绑定环境ID
	BoundEnvCode string `json:"bound_env_code"` // 绑定环境代码
	Reason       string `json:"reason"`         // 疑异原因（多信号以 ";" 连接）
}

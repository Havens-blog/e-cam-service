package domain

// DefaultSyncAssetTypes 默认同步的资产类型清单。
// 账号未显式配置 SupportedAssetTypes 时，自动同步按此清单执行。
//
// 注意：dns 必须包含在内。DNS 是账号级全局资产（不按地域），历史上
// 该清单曾遗漏 dns，导致默认配置的账号 DNS 域名/解析记录永远不会被
// 自动同步，仅在手动触发时才更新（auto_sync.go 旧硬编码清单缺陷）。
// 执行器侧 expandAssetTypes 对 "dns" 原样透传并触发 syncDNS。
var DefaultSyncAssetTypes = []string{
	// 计算
	"ecs", "disk", "snapshot", "security_group", "image",
	// 数据库
	"rds", "redis", "mongodb",
	// 网络（dns 为账号级全局资产）
	"vpc", "vswitch", "eip", "lb", "cdn", "waf", "dns",
	// 存储
	"nas", "oss",
	// 中间件
	"kafka", "elasticsearch",
}

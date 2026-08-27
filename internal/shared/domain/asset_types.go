package domain

// DefaultSyncAssetTypes 默认同步的资产类型清单。
// 账号未显式配置 SupportedAssetTypes 时，自动同步按此清单执行。
// 注意：dns 必须包含在内。DNS 是账号级全局资产（不按地域），历史上
// 该清单曾遗漏 dns，导致默认配置的账号 DNS 域名/解析记录永远不会被
// 自动同步，仅在手动触发时才更新。
var DefaultSyncAssetTypes = []string{
	// 计算
	"ecs", "disk", "snapshot", "security_group", "image",
	// 数据库
	"rds", "redis", "mongodb",
	// 网络（dns 是账号级全局资产）
	"vpc", "vswitch", "eip", "lb", "cdn", "waf", "dns",
	// 存储
	"nas", "oss",
	// 中间件
	"kafka", "elasticsearch",
}

// 聚合类型展开清单（compute/database/network/storage/middleware 五个聚合
// 别名展开到的子类型）。expandAssetTypes（executor）与 asset_sync.go 中
// 语义相同的展开必须引用这些常量，避免多处硬编码各自漂移。
var (
	ComputeAssetTypes    = []string{"ecs", "disk", "snapshot", "security_group", "image"}
	DatabaseAssetTypes   = []string{"rds", "redis", "mongodb"}
	NetworkAssetTypes    = []string{"vpc", "vswitch", "eip", "eni", "lb", "cdn", "waf", "dns"}
	StorageAssetTypes    = []string{"nas", "oss"}
	MiddlewareAssetTypes = []string{"kafka", "elasticsearch"}
)

// DefaultCMDBSyncAssetTypes CMDB 同步服务（asset_sync.go，API 直调路径）的
// 默认资产类型清单。与 DefaultSyncAssetTypes 是两种语义、且历史上已漂移：
// 本清单含 eni、不含 vswitch/kafka/elasticsearch。为保持行为等价不做合并，
// 漂移是否收敛由后续任务单独决策。
var DefaultCMDBSyncAssetTypes = []string{
	// 计算
	"ecs", "disk", "snapshot", "security_group", "image",
	// 数据库
	"rds", "redis", "mongodb",
	// 网络
	"vpc", "eip", "lb", "cdn", "waf", "eni", "dns",
	// 存储
	"nas", "oss",
}

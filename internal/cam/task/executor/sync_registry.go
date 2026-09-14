// 文件：sync_registry.go
//
// 作用：syncRegionAssets 的资源类型 → 同步函数派发表（同步收敛 Phase 2 S5 表驱动重构）。
// 将原先 syncRegionAssets 内 20-case 的 switch 收敛为两张注册表：
//   - assetSyncFns  使用 asset.CloudAssetAdapter（计算型资源，目前仅 ECS，无需 cloudx）；
//   - cloudxSyncFns 使用 cloudx.CloudAdapter（其余数据库/网络/存储类资源，按需懒加载）。
//
// 行为与旧 switch 一致：
//   - 别名（elasticsearch/es、security_group/securitygroup/sg、vswitch/subnet）路由到同一函数；
//   - 每类型保留独立失败日志标签（如同步RDS失败）；
//   - dns 不在两张表中，由 syncingAssets 循环显式跳过（账号级处理）；
//   - 未注册类型进入 default 的"不支持的资源类型"告警。
package executor

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/asset"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// dnsAssetType DNS 资源类型：全局服务，不在地域级同步（账号级处理，见 Execute 中的 syncDNS）。
const dnsAssetType = "dns"

// assetSyncEntry asset 适配器派发条目：同步函数 + 失败日志标签。
type assetSyncEntry struct {
	fn    func(e *SyncAssetsExecutor, ctx context.Context, adapter asset.CloudAssetAdapter, account *domain.CloudAccount, region string) (int, error)
	label string
}

// cloudxSyncEntry cloudx 适配器派发条目：同步函数 + 失败日志标签。
type cloudxSyncEntry struct {
	fn    func(e *SyncAssetsExecutor, ctx context.Context, adapter cloudx.CloudAdapter, account *domain.CloudAccount, region string) (int, error)
	label string
}

// assetSyncFns asset 适配器派发表（计算型资源，无需 cloudx，直接同步）。
var assetSyncFns = map[string]assetSyncEntry{
	"ecs": {(*SyncAssetsExecutor).syncRegionECS, "同步ECS失败"},
}

// cloudxSyncFns cloudx 适配器派发表，key 为 expandAssetTypes 展开后的资源类型（含别名）。
var cloudxSyncFns = map[string]cloudxSyncEntry{
	"rds":            {(*SyncAssetsExecutor).syncRegionRDS, "同步RDS失败"},
	"redis":          {(*SyncAssetsExecutor).syncRegionRedis, "同步Redis失败"},
	"mongodb":        {(*SyncAssetsExecutor).syncRegionMongoDB, "同步MongoDB失败"},
	"vpc":            {(*SyncAssetsExecutor).syncRegionVPC, "同步VPC失败"},
	"eip":            {(*SyncAssetsExecutor).syncRegionEIP, "同步EIP失败"},
	"eni":            {(*SyncAssetsExecutor).syncRegionENI, "同步ENI失败"},
	"lb":             {(*SyncAssetsExecutor).syncRegionLB, "同步LB失败"},
	"nas":            {(*SyncAssetsExecutor).syncRegionNAS, "同步NAS失败"},
	"oss":            {(*SyncAssetsExecutor).syncRegionOSS, "同步OSS失败"},
	"kafka":          {(*SyncAssetsExecutor).syncRegionKafka, "同步Kafka失败"},
	"elasticsearch":  {(*SyncAssetsExecutor).syncRegionElasticsearch, "同步Elasticsearch失败"},
	"es":             {(*SyncAssetsExecutor).syncRegionElasticsearch, "同步Elasticsearch失败"},
	"disk":           {(*SyncAssetsExecutor).syncRegionDisk, "同步云盘失败"},
	"snapshot":       {(*SyncAssetsExecutor).syncRegionSnapshot, "同步快照失败"},
	"security_group": {(*SyncAssetsExecutor).syncRegionSecurityGroup, "同步安全组失败"},
	"securitygroup":  {(*SyncAssetsExecutor).syncRegionSecurityGroup, "同步安全组失败"},
	"sg":             {(*SyncAssetsExecutor).syncRegionSecurityGroup, "同步安全组失败"},
	"image":          {(*SyncAssetsExecutor).syncRegionImage, "同步镜像失败"},
	"vswitch":        {(*SyncAssetsExecutor).syncRegionVSwitch, "同步VSwitch失败"},
	"subnet":         {(*SyncAssetsExecutor).syncRegionVSwitch, "同步VSwitch失败"},
	"cdn":            {(*SyncAssetsExecutor).syncRegionCDN, "同步CDN失败"},
	"waf":            {(*SyncAssetsExecutor).syncRegionWAF, "同步WAF失败"},
}

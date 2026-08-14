package web

import (
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// AssetHandler 资产HTTP处理器
// 按资产类型提供RESTful风格的API
type AssetHandler struct {
	instanceSvc service.InstanceService
	logger      *elog.Component
}

// NewAssetHandler 创建资产处理器
func NewAssetHandler(instanceSvc service.InstanceService) *AssetHandler {
	return &AssetHandler{
		instanceSvc: instanceSvc,
		logger:      elog.DefaultLogger,
	}
}

// RegisterRoutes 注册资产路由 (旧方式，保留兼容)
// Deprecated: 请使用 RegisterRoutesWithGroup
func (h *AssetHandler) RegisterRoutes(rg *gin.RouterGroup) {
	assetsGroup := rg.Group("/assets")
	h.registerAssetRoutes(assetsGroup)
}

// RegisterRoutesWithGroup 注册资产路由到指定路由组
// 用于外部已配置好中间件的路由组
func (h *AssetHandler) RegisterRoutesWithGroup(assetsGroup *gin.RouterGroup) {
	h.registerAssetRoutes(assetsGroup)
}

// registerAssetRoutes 内部路由注册方法
func (h *AssetHandler) registerAssetRoutes(assetsGroup *gin.RouterGroup) {
	// 统一搜索
	assetsGroup.GET("/search", h.Search)

	// ECS 云虚拟机
	assetsGroup.GET("/ecs", h.ListECS)
	assetsGroup.GET("/ecs/:asset_id", h.GetECS)
	assetsGroup.GET("/ecs/:asset_id/relations", h.GetECSRelations)

	// 云盘
	assetsGroup.GET("/disk", h.ListDisk)
	assetsGroup.GET("/disk/:asset_id", h.GetDisk)

	// 快照
	assetsGroup.GET("/snapshot", h.ListSnapshot)
	assetsGroup.GET("/snapshot/:asset_id", h.GetSnapshot)

	// 安全组
	assetsGroup.GET("/security-group", h.ListSecurityGroup)
	assetsGroup.GET("/security-group/:asset_id", h.GetSecurityGroup)

	// RDS 关系型数据库
	assetsGroup.GET("/rds", h.ListRDS)
	assetsGroup.GET("/rds/:asset_id", h.GetRDS)

	// Redis 缓存
	assetsGroup.GET("/redis", h.ListRedis)
	assetsGroup.GET("/redis/:asset_id", h.GetRedis)

	// MongoDB 文档数据库
	assetsGroup.GET("/mongodb", h.ListMongoDB)
	assetsGroup.GET("/mongodb/:asset_id", h.GetMongoDB)

	// VPC 虚拟私有云
	assetsGroup.GET("/vpc", h.ListVPC)
	assetsGroup.GET("/vpc/:asset_id", h.GetVPC)

	// EIP 弹性公网IP
	assetsGroup.GET("/eip", h.ListEIP)
	assetsGroup.GET("/eip/:asset_id", h.GetEIP)

	// VSwitch 交换机/子网
	assetsGroup.GET("/vswitch", h.ListVSwitch)
	assetsGroup.GET("/vswitch/:asset_id", h.GetVSwitch)

	// LB 负载均衡
	assetsGroup.GET("/lb", h.ListLB)
	assetsGroup.GET("/lb/:asset_id", h.GetLB)

	// CDN 内容分发网络
	assetsGroup.GET("/cdn", h.ListCDN)
	assetsGroup.GET("/cdn/:asset_id", h.GetCDN)

	// WAF Web应用防火墙
	assetsGroup.GET("/waf", h.ListWAF)
	assetsGroup.GET("/waf/:asset_id", h.GetWAF)

	// ENI 弹性网卡
	assetsGroup.GET("/eni", h.ListENI)
	assetsGroup.GET("/eni/:asset_id", h.GetENI)

	// NAS 文件存储
	assetsGroup.GET("/nas", h.ListNAS)
	assetsGroup.GET("/nas/:asset_id", h.GetNAS)

	// OSS 对象存储
	assetsGroup.GET("/oss", h.ListOSS)
	assetsGroup.GET("/oss/:asset_id", h.GetOSS)

	// Kafka 消息队列
	assetsGroup.GET("/kafka", h.ListKafka)
	assetsGroup.GET("/kafka/:asset_id", h.GetKafka)

	// Elasticsearch 搜索服务
	assetsGroup.GET("/elasticsearch", h.ListElasticsearch)
	assetsGroup.GET("/elasticsearch/:asset_id", h.GetElasticsearch)

	// 镜像
	assetsGroup.GET("/image", h.ListImage)
	assetsGroup.GET("/image/stats", h.GetImageStats)
	assetsGroup.GET("/image/:asset_id", h.GetImage)
}

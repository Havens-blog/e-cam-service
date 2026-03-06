package domain

// CloudProvider 云厂商标识
type CloudProvider string

const (
	CloudProviderAliyun  CloudProvider = "aliyun"  // 阿里云
	CloudProviderAWS     CloudProvider = "aws"     // Amazon Web Services
	CloudProviderVolcano CloudProvider = "volcano" // 火山引擎
	CloudProviderHuawei  CloudProvider = "huawei"  // 华为云
	CloudProviderTencent CloudProvider = "tencent" // 腾讯云
)

// ServiceType 统一服务分类常量
const (
	ServiceTypeCompute    = "compute"    // 计算
	ServiceTypeStorage    = "storage"    // 存储
	ServiceTypeNetwork    = "network"    // 网络
	ServiceTypeDatabase   = "database"   // 数据库
	ServiceTypeMiddleware = "middleware" // 中间件
	ServiceTypeOther      = "other"      // 其他
)

// AllocationDimensionType 分摊维度类型常量
const (
	DimDepartment    = "department"     // 部门
	DimResourceGroup = "resource_group" // 资源组
	DimProject       = "project"        // 所属项目
	DimTag           = "tag"            // 标签
	DimCloudAccount  = "cloud_account"  // 云账号
	DimRegion        = "region"         // 地域
	DimServiceType   = "service_type"   // 服务类型
)

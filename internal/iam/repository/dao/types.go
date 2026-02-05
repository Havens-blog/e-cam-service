package dao

// CloudProvider 云厂商枚举
type CloudProvider string

const (
	CloudProviderAliyun  CloudProvider = "aliyun"  // 阿里云
	CloudProviderAWS     CloudProvider = "aws"     // Amazon Web Services
	CloudProviderHuawei  CloudProvider = "huawei"  // 华为云
	CloudProviderTencent CloudProvider = "tencent" // 腾讯云
	CloudProviderVolcano CloudProvider = "volcano" // 火山云
)

package normalizer

import (
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// ServiceTypeMapper 将各云厂商原始服务类型映射为统一分类
type ServiceTypeMapper struct {
	// mappings key: CloudProvider, value: map[lowercase原始服务类型]统一分类
	mappings map[shareddomain.CloudProvider]map[string]string
}

// NewServiceTypeMapper 创建服务类型映射器
func NewServiceTypeMapper() *ServiceTypeMapper {
	m := &ServiceTypeMapper{
		mappings: make(map[shareddomain.CloudProvider]map[string]string),
	}
	m.initAliyun()
	m.initAWS()
	m.initVolcano()
	m.initHuawei()
	m.initTencent()
	return m
}

// Map 将云厂商原始服务类型映射为统一分类
// 未知类型返回 "other"
func (m *ServiceTypeMapper) Map(provider shareddomain.CloudProvider, rawServiceType string) string {
	providerMap, ok := m.mappings[provider]
	if !ok {
		return domain.ServiceTypeOther
	}
	unified, ok := providerMap[strings.ToLower(rawServiceType)]
	if !ok {
		return domain.ServiceTypeOther
	}
	return unified
}

func (m *ServiceTypeMapper) initAliyun() {
	m.mappings[shareddomain.CloudProviderAliyun] = map[string]string{
		// compute
		"ecs":  domain.ServiceTypeCompute,
		"fc":   domain.ServiceTypeCompute,
		"ehpc": domain.ServiceTypeCompute,
		"ack":  domain.ServiceTypeCompute,
		"swas": domain.ServiceTypeCompute,
		// storage
		"oss":    domain.ServiceTypeStorage,
		"nas":    domain.ServiceTypeStorage,
		"alidfs": domain.ServiceTypeStorage,
		// network
		"slb": domain.ServiceTypeNetwork,
		"alb": domain.ServiceTypeNetwork,
		"nlb": domain.ServiceTypeNetwork,
		"vpc": domain.ServiceTypeNetwork,
		"eip": domain.ServiceTypeNetwork,
		"cdn": domain.ServiceTypeNetwork,
		"nat": domain.ServiceTypeNetwork,
		"ga":  domain.ServiceTypeNetwork,
		// database
		"rds":     domain.ServiceTypeDatabase,
		"redis":   domain.ServiceTypeDatabase,
		"mongodb": domain.ServiceTypeDatabase,
		"polardb": domain.ServiceTypeDatabase,
		"dds":     domain.ServiceTypeDatabase,
		"hbase":   domain.ServiceTypeDatabase,
		// middleware
		"kafka":         domain.ServiceTypeMiddleware,
		"elasticsearch": domain.ServiceTypeMiddleware,
		"mq":            domain.ServiceTypeMiddleware,
		"rocketmq":      domain.ServiceTypeMiddleware,
		"rabbitmq":      domain.ServiceTypeMiddleware,
	}
}

func (m *ServiceTypeMapper) initAWS() {
	m.mappings[shareddomain.CloudProviderAWS] = map[string]string{
		// compute
		"amazon elastic compute cloud":      domain.ServiceTypeCompute,
		"amazon ec2":                        domain.ServiceTypeCompute,
		"aws lambda":                        domain.ServiceTypeCompute,
		"amazon elastic container service":  domain.ServiceTypeCompute,
		"amazon elastic kubernetes service": domain.ServiceTypeCompute,
		// storage
		"amazon simple storage service": domain.ServiceTypeStorage,
		"amazon s3":                     domain.ServiceTypeStorage,
		"amazon elastic block store":    domain.ServiceTypeStorage,
		"amazon ebs":                    domain.ServiceTypeStorage,
		"amazon elastic file system":    domain.ServiceTypeStorage,
		// network
		"amazon virtual private cloud": domain.ServiceTypeNetwork,
		"amazon vpc":                   domain.ServiceTypeNetwork,
		"amazon cloudfront":            domain.ServiceTypeNetwork,
		"elastic load balancing":       domain.ServiceTypeNetwork,
		"amazon route 53":              domain.ServiceTypeNetwork,
		"aws global accelerator":       domain.ServiceTypeNetwork,
		// database
		"amazon relational database service": domain.ServiceTypeDatabase,
		"amazon rds":                         domain.ServiceTypeDatabase,
		"amazon dynamodb":                    domain.ServiceTypeDatabase,
		"amazon elasticache":                 domain.ServiceTypeDatabase,
		"amazon redshift":                    domain.ServiceTypeDatabase,
		"amazon documentdb":                  domain.ServiceTypeDatabase,
		// middleware
		"amazon managed streaming for apache kafka": domain.ServiceTypeMiddleware,
		"amazon msk":                domain.ServiceTypeMiddleware,
		"amazon opensearch service": domain.ServiceTypeMiddleware,
		"amazon mq":                 domain.ServiceTypeMiddleware,
		"amazon sqs":                domain.ServiceTypeMiddleware,
		"amazon sns":                domain.ServiceTypeMiddleware,
	}
}

func (m *ServiceTypeMapper) initVolcano() {
	m.mappings[shareddomain.CloudProviderVolcano] = map[string]string{
		// compute
		"ecs":          domain.ServiceTypeCompute,
		"vecomputeecs": domain.ServiceTypeCompute,
		// storage
		"tos": domain.ServiceTypeStorage,
		"ebs": domain.ServiceTypeStorage,
		// network
		"clb": domain.ServiceTypeNetwork,
		"alb": domain.ServiceTypeNetwork,
		"vpc": domain.ServiceTypeNetwork,
		"eip": domain.ServiceTypeNetwork,
		"cdn": domain.ServiceTypeNetwork,
		"nat": domain.ServiceTypeNetwork,
		// database
		"rds_mysql": domain.ServiceTypeDatabase,
		"redis":     domain.ServiceTypeDatabase,
		"mongodb":   domain.ServiceTypeDatabase,
		// middleware
		"kafka":         domain.ServiceTypeMiddleware,
		"elasticsearch": domain.ServiceTypeMiddleware,
	}
}

func (m *ServiceTypeMapper) initHuawei() {
	m.mappings[shareddomain.CloudProviderHuawei] = map[string]string{
		// compute
		"hws.service.type.ec2": domain.ServiceTypeCompute,
		"hws.service.type.ecs": domain.ServiceTypeCompute,
		"hws.service.type.cce": domain.ServiceTypeCompute,
		// storage
		"hws.service.type.obs": domain.ServiceTypeStorage,
		"hws.service.type.evs": domain.ServiceTypeStorage,
		"hws.service.type.sfs": domain.ServiceTypeStorage,
		// network
		"hws.service.type.vpc": domain.ServiceTypeNetwork,
		"hws.service.type.elb": domain.ServiceTypeNetwork,
		"hws.service.type.eip": domain.ServiceTypeNetwork,
		"hws.service.type.cdn": domain.ServiceTypeNetwork,
		"hws.service.type.nat": domain.ServiceTypeNetwork,
		// database
		"hws.service.type.rds":     domain.ServiceTypeDatabase,
		"hws.service.type.dcs":     domain.ServiceTypeDatabase,
		"hws.service.type.dds":     domain.ServiceTypeDatabase,
		"hws.service.type.gaussdb": domain.ServiceTypeDatabase,
		// middleware
		"hws.service.type.kafka":    domain.ServiceTypeMiddleware,
		"hws.service.type.css":      domain.ServiceTypeMiddleware,
		"hws.service.type.rabbitmq": domain.ServiceTypeMiddleware,
	}
}

func (m *ServiceTypeMapper) initTencent() {
	m.mappings[shareddomain.CloudProviderTencent] = map[string]string{
		// compute
		"cvm":        domain.ServiceTypeCompute,
		"scf":        domain.ServiceTypeCompute,
		"tke":        domain.ServiceTypeCompute,
		"lighthouse": domain.ServiceTypeCompute,
		// storage
		"cos": domain.ServiceTypeStorage,
		"cbs": domain.ServiceTypeStorage,
		"cfs": domain.ServiceTypeStorage,
		// network
		"clb": domain.ServiceTypeNetwork,
		"vpc": domain.ServiceTypeNetwork,
		"eip": domain.ServiceTypeNetwork,
		"cdn": domain.ServiceTypeNetwork,
		"nat": domain.ServiceTypeNetwork,
		// database
		"cdb":     domain.ServiceTypeDatabase,
		"redis":   domain.ServiceTypeDatabase,
		"mongodb": domain.ServiceTypeDatabase,
		"cynosdb": domain.ServiceTypeDatabase,
		"mariadb": domain.ServiceTypeDatabase,
		// middleware
		"ckafka": domain.ServiceTypeMiddleware,
		"es":     domain.ServiceTypeMiddleware,
		"tdmq":   domain.ServiceTypeMiddleware,
		"cmq":    domain.ServiceTypeMiddleware,
	}
}

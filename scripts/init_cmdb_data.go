//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ModelGroup 模型分组
type ModelGroup struct {
	ID          int64     `bson:"id"`
	UID         string    `bson:"uid"`
	Name        string    `bson:"name"`
	Icon        string    `bson:"icon"`
	SortOrder   int       `bson:"sort_order"`
	IsBuiltin   bool      `bson:"is_builtin"`
	Description string    `bson:"description"`
	CreateTime  time.Time `bson:"create_time"`
	UpdateTime  time.Time `bson:"update_time"`
}

// Model 模型定义
type Model struct {
	ID           int64  `bson:"id"`
	UID          string `bson:"uid"`
	Name         string `bson:"name"`
	ModelGroupID int64  `bson:"model_group_id"`
	ParentUID    string `bson:"parent_uid"`
	Category     string `bson:"category"`
	Level        int    `bson:"level"`
	Icon         string `bson:"icon"`
	Description  string `bson:"description"`
	Provider     string `bson:"provider"` // all 表示通用模型
	Extensible   bool   `bson:"extensible"`
	Ctime        int64  `bson:"ctime"`
	Utime        int64  `bson:"utime"`
}

// AttributeGroup 属性分组
type AttributeGroup struct {
	ID          int64  `bson:"id"`
	UID         string `bson:"uid"`
	Name        string `bson:"name"`
	ModelUID    string `bson:"model_uid"`
	Index       int    `bson:"index"`
	IsBuiltin   bool   `bson:"is_builtin"`
	Description string `bson:"description"`
	Ctime       int64  `bson:"ctime"`
	Utime       int64  `bson:"utime"`
}

// Attribute 模型属性
type Attribute struct {
	ID          int64  `bson:"id"`
	FieldUID    string `bson:"field_uid"`
	FieldName   string `bson:"field_name"`
	FieldType   string `bson:"field_type"`
	ModelUID    string `bson:"model_uid"`
	GroupID     int64  `bson:"group_id"`
	DisplayName string `bson:"display_name"`
	Display     bool   `bson:"display"`
	Index       int    `bson:"index"`
	Required    bool   `bson:"required"`
	Secure      bool   `bson:"secure"`
	Link        bool   `bson:"link"`
	LinkModel   string `bson:"link_model"`
	Option      string `bson:"option"`
	Ctime       int64  `bson:"ctime"`
	Utime       int64  `bson:"utime"`
}

// ModelRelation 模型关系
type ModelRelation struct {
	ID             int64  `bson:"id"`
	UID            string `bson:"uid"`
	Name           string `bson:"name"`
	SourceModelUID string `bson:"source_model_uid"`
	TargetModelUID string `bson:"target_model_uid"`
	RelationType   string `bson:"relation_type"`
	Direction      string `bson:"direction"`
	Description    string `bson:"description"`
	Ctime          int64  `bson:"ctime"`
	Utime          int64  `bson:"utime"`
}

// 分组ID常量
const (
	GROUP_HOST       = 1
	GROUP_COMPUTE    = 2
	GROUP_NETWORK    = 3
	GROUP_DATABASE   = 4
	GROUP_MIDDLEWARE = 5
	GROUP_CONTAINER  = 6
	GROUP_STORAGE    = 7
	GROUP_SECURITY   = 8
	GROUP_IAM        = 9
	GROUP_CUSTOM     = 10
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// MongoDB连接配置
	credential := options.Credential{
		Username:   "ecmdb",
		Password:   "123456",
		AuthSource: "admin",
	}
	clientOpts := options.Client().
		ApplyURI("mongodb://106.52.187.69:27017").
		SetAuth(credential)

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)

	db := client.Database("ecmdb")
	now := time.Now()
	nowMs := now.UnixMilli()

	// 初始化模型分组
	groups := getModelGroups(now)
	fmt.Println("Inserting model groups...")
	for _, g := range groups {
		_, err := db.Collection("c_model_group").UpdateOne(ctx,
			bson.M{"uid": g.UID},
			bson.M{"$set": g},
			options.Update().SetUpsert(true))
		if err != nil {
			log.Printf("Failed to insert group %s: %v", g.UID, err)
		}
	}

	// 初始化模型
	models := getModels(nowMs)
	fmt.Println("Inserting models...")
	for _, m := range models {
		_, err := db.Collection("c_model").UpdateOne(ctx,
			bson.M{"uid": m.UID},
			bson.M{"$set": m},
			options.Update().SetUpsert(true))
		if err != nil {
			log.Printf("Failed to insert model %s: %v", m.UID, err)
		}
	}

	// 初始化属性分组
	attrGroups := getAttributeGroups(nowMs)
	fmt.Println("Inserting attribute groups...")
	for _, g := range attrGroups {
		_, err := db.Collection("c_attribute_group").UpdateOne(ctx,
			bson.M{"model_uid": g.ModelUID, "uid": g.UID},
			bson.M{"$set": g},
			options.Update().SetUpsert(true))
		if err != nil {
			log.Printf("Failed to insert attribute group %s.%s: %v", g.ModelUID, g.UID, err)
		}
	}

	// 初始化属性
	attrs := getAttributes(nowMs)
	fmt.Println("Inserting attributes...")
	for _, a := range attrs {
		_, err := db.Collection("c_attribute").UpdateOne(ctx,
			bson.M{"model_uid": a.ModelUID, "field_uid": a.FieldUID},
			bson.M{"$set": a},
			options.Update().SetUpsert(true))
		if err != nil {
			log.Printf("Failed to insert attribute %s.%s: %v", a.ModelUID, a.FieldUID, err)
		}
	}

	// 初始化模型关系
	relations := getModelRelations(nowMs)
	fmt.Println("Inserting model relations...")
	for _, r := range relations {
		_, err := db.Collection("c_model_relation_type").UpdateOne(ctx,
			bson.M{"uid": r.UID},
			bson.M{"$set": r},
			options.Update().SetUpsert(true))
		if err != nil {
			log.Printf("Failed to insert relation %s: %v", r.UID, err)
		}
	}

	fmt.Println("Done! CMDB data initialized successfully.")
}

func getModelGroups(now time.Time) []ModelGroup {
	return []ModelGroup{
		{ID: GROUP_HOST, UID: "host", Name: "主机管理", Icon: "server", SortOrder: 1, IsBuiltin: true, Description: "物理机、虚拟机等主机资源", CreateTime: now, UpdateTime: now},
		{ID: GROUP_COMPUTE, UID: "compute", Name: "计算资源", Icon: "cloud", SortOrder: 2, IsBuiltin: true, Description: "云服务器、弹性计算等", CreateTime: now, UpdateTime: now},
		{ID: GROUP_NETWORK, UID: "network", Name: "网络资源", Icon: "network", SortOrder: 3, IsBuiltin: true, Description: "VPC、子网、负载均衡等", CreateTime: now, UpdateTime: now},
		{ID: GROUP_DATABASE, UID: "database", Name: "数据库", Icon: "database", SortOrder: 4, IsBuiltin: true, Description: "关系型数据库、NoSQL等", CreateTime: now, UpdateTime: now},
		{ID: GROUP_MIDDLEWARE, UID: "middleware", Name: "中间件", Icon: "middleware", SortOrder: 5, IsBuiltin: true, Description: "消息队列、缓存等", CreateTime: now, UpdateTime: now},
		{ID: GROUP_CONTAINER, UID: "container", Name: "容器服务", Icon: "container", SortOrder: 6, IsBuiltin: true, Description: "Kubernetes集群、容器等", CreateTime: now, UpdateTime: now},
		{ID: GROUP_STORAGE, UID: "storage", Name: "存储资源", Icon: "storage", SortOrder: 7, IsBuiltin: true, Description: "对象存储、块存储等", CreateTime: now, UpdateTime: now},
		{ID: GROUP_SECURITY, UID: "security", Name: "安全资源", Icon: "security", SortOrder: 8, IsBuiltin: true, Description: "安全组、防火墙等", CreateTime: now, UpdateTime: now},
		{ID: GROUP_IAM, UID: "iam", Name: "身份权限", Icon: "user", SortOrder: 9, IsBuiltin: true, Description: "用户、角色、策略等", CreateTime: now, UpdateTime: now},
		{ID: GROUP_CUSTOM, UID: "custom", Name: "自定义", Icon: "custom", SortOrder: 100, IsBuiltin: true, Description: "用户自定义模型", CreateTime: now, UpdateTime: now},
	}
}

func getModels(now int64) []Model {
	return []Model{
		// ========== 主机管理 ==========
		{ID: 1, UID: "host", Name: "主机", ModelGroupID: GROUP_HOST, Category: "host", Level: 1, Icon: "server", Provider: "all", Extensible: true, Description: "物理机或虚拟机", Ctime: now, Utime: now},

		// ========== 计算资源 ==========
		{ID: 10, UID: "cloud_vm", Name: "虚拟机", ModelGroupID: GROUP_COMPUTE, Category: "compute", Level: 1, Icon: "server", Provider: "all", Extensible: true, Description: "云服务器/虚拟机（通用模型）", Ctime: now, Utime: now},
		{ID: 11, UID: "ecs", Name: "云服务器", ModelGroupID: GROUP_COMPUTE, ParentUID: "cloud_vm", Category: "compute", Level: 2, Icon: "ecs", Provider: "all", Extensible: true, Description: "弹性云服务器实例", Ctime: now, Utime: now},

		// ========== 网络资源 ==========
		{ID: 20, UID: "vpc", Name: "VPC", ModelGroupID: GROUP_NETWORK, Category: "network", Level: 1, Icon: "vpc", Provider: "all", Extensible: true, Description: "虚拟私有云/专有网络", Ctime: now, Utime: now},
		{ID: 21, UID: "subnet", Name: "子网", ModelGroupID: GROUP_NETWORK, Category: "network", Level: 2, ParentUID: "vpc", Icon: "subnet", Provider: "all", Extensible: true, Description: "子网/交换机", Ctime: now, Utime: now},
		{ID: 22, UID: "eip", Name: "弹性公网IP", ModelGroupID: GROUP_NETWORK, Category: "network", Level: 1, Icon: "eip", Provider: "all", Extensible: true, Description: "弹性公网IP", Ctime: now, Utime: now},
		{ID: 23, UID: "lb", Name: "负载均衡", ModelGroupID: GROUP_NETWORK, Category: "network", Level: 1, Icon: "lb", Provider: "all", Extensible: true, Description: "负载均衡器", Ctime: now, Utime: now},
		{ID: 24, UID: "nat", Name: "NAT网关", ModelGroupID: GROUP_NETWORK, Category: "network", Level: 1, Icon: "nat", Provider: "all", Extensible: true, Description: "NAT网关", Ctime: now, Utime: now},
		{ID: 25, UID: "route_table", Name: "路由表", ModelGroupID: GROUP_NETWORK, Category: "network", Level: 2, ParentUID: "vpc", Icon: "route", Provider: "all", Extensible: true, Description: "路由表", Ctime: now, Utime: now},

		// ========== 数据库 ==========
		{ID: 30, UID: "rds", Name: "关系型数据库", ModelGroupID: GROUP_DATABASE, Category: "database", Level: 1, Icon: "rds", Provider: "all", Extensible: true, Description: "MySQL/PostgreSQL/SQLServer等", Ctime: now, Utime: now},
		{ID: 31, UID: "redis", Name: "Redis", ModelGroupID: GROUP_DATABASE, Category: "database", Level: 1, Icon: "redis", Provider: "all", Extensible: true, Description: "Redis缓存数据库", Ctime: now, Utime: now},
		{ID: 32, UID: "mongodb", Name: "MongoDB", ModelGroupID: GROUP_DATABASE, Category: "database", Level: 1, Icon: "mongodb", Provider: "all", Extensible: true, Description: "MongoDB文档数据库", Ctime: now, Utime: now},
		{ID: 33, UID: "elasticsearch", Name: "Elasticsearch", ModelGroupID: GROUP_DATABASE, Category: "database", Level: 1, Icon: "es", Provider: "all", Extensible: true, Description: "Elasticsearch搜索引擎", Ctime: now, Utime: now},

		// ========== 中间件 ==========
		{ID: 40, UID: "kafka", Name: "Kafka", ModelGroupID: GROUP_MIDDLEWARE, Category: "middleware", Level: 1, Icon: "kafka", Provider: "all", Extensible: true, Description: "Kafka消息队列", Ctime: now, Utime: now},
		{ID: 41, UID: "rabbitmq", Name: "RabbitMQ", ModelGroupID: GROUP_MIDDLEWARE, Category: "middleware", Level: 1, Icon: "rabbitmq", Provider: "all", Extensible: true, Description: "RabbitMQ消息队列", Ctime: now, Utime: now},
		{ID: 42, UID: "rocketmq", Name: "RocketMQ", ModelGroupID: GROUP_MIDDLEWARE, Category: "middleware", Level: 1, Icon: "rocketmq", Provider: "all", Extensible: true, Description: "RocketMQ消息队列", Ctime: now, Utime: now},

		// ========== 容器服务 ==========
		{ID: 50, UID: "k8s_cluster", Name: "K8s集群", ModelGroupID: GROUP_CONTAINER, Category: "container", Level: 1, Icon: "kubernetes", Provider: "all", Extensible: true, Description: "Kubernetes集群", Ctime: now, Utime: now},
		{ID: 51, UID: "k8s_namespace", Name: "命名空间", ModelGroupID: GROUP_CONTAINER, Category: "container", Level: 2, ParentUID: "k8s_cluster", Icon: "namespace", Provider: "all", Extensible: true, Description: "K8s命名空间", Ctime: now, Utime: now},
		{ID: 52, UID: "k8s_deployment", Name: "Deployment", ModelGroupID: GROUP_CONTAINER, Category: "container", Level: 2, ParentUID: "k8s_namespace", Icon: "deployment", Provider: "all", Extensible: true, Description: "K8s Deployment", Ctime: now, Utime: now},
		{ID: 53, UID: "k8s_service", Name: "Service", ModelGroupID: GROUP_CONTAINER, Category: "container", Level: 2, ParentUID: "k8s_namespace", Icon: "service", Provider: "all", Extensible: true, Description: "K8s Service", Ctime: now, Utime: now},
		{ID: 54, UID: "k8s_pod", Name: "Pod", ModelGroupID: GROUP_CONTAINER, Category: "container", Level: 2, ParentUID: "k8s_namespace", Icon: "pod", Provider: "all", Extensible: true, Description: "K8s Pod", Ctime: now, Utime: now},
		{ID: 55, UID: "k8s_node", Name: "Node", ModelGroupID: GROUP_CONTAINER, Category: "container", Level: 2, ParentUID: "k8s_cluster", Icon: "node", Provider: "all", Extensible: true, Description: "K8s Node", Ctime: now, Utime: now},

		// ========== 存储资源 ==========
		{ID: 60, UID: "oss", Name: "对象存储", ModelGroupID: GROUP_STORAGE, Category: "storage", Level: 1, Icon: "oss", Provider: "all", Extensible: true, Description: "对象存储服务", Ctime: now, Utime: now},
		{ID: 61, UID: "disk", Name: "云盘", ModelGroupID: GROUP_STORAGE, Category: "storage", Level: 1, Icon: "disk", Provider: "all", Extensible: true, Description: "块存储/云硬盘", Ctime: now, Utime: now},
		{ID: 62, UID: "nas", Name: "文件存储", ModelGroupID: GROUP_STORAGE, Category: "storage", Level: 1, Icon: "nas", Provider: "all", Extensible: true, Description: "NAS文件存储", Ctime: now, Utime: now},

		// ========== 安全资源 ==========
		{ID: 70, UID: "security_group", Name: "安全组", ModelGroupID: GROUP_SECURITY, Category: "security", Level: 1, Icon: "security", Provider: "all", Extensible: true, Description: "安全组", Ctime: now, Utime: now},
		{ID: 71, UID: "acl", Name: "访问控制", ModelGroupID: GROUP_SECURITY, Category: "security", Level: 1, Icon: "acl", Provider: "all", Extensible: true, Description: "网络ACL", Ctime: now, Utime: now},
		{ID: 72, UID: "certificate", Name: "SSL证书", ModelGroupID: GROUP_SECURITY, Category: "security", Level: 1, Icon: "cert", Provider: "all", Extensible: true, Description: "SSL/TLS证书", Ctime: now, Utime: now},

		// ========== 身份权限 ==========
		{ID: 80, UID: "iam_user", Name: "IAM用户", ModelGroupID: GROUP_IAM, Category: "iam", Level: 1, Icon: "user", Provider: "all", Extensible: true, Description: "云账号子用户", Ctime: now, Utime: now},
		{ID: 81, UID: "iam_group", Name: "IAM用户组", ModelGroupID: GROUP_IAM, Category: "iam", Level: 1, Icon: "group", Provider: "all", Extensible: true, Description: "用户组", Ctime: now, Utime: now},
		{ID: 82, UID: "iam_role", Name: "IAM角色", ModelGroupID: GROUP_IAM, Category: "iam", Level: 1, Icon: "role", Provider: "all", Extensible: true, Description: "角色", Ctime: now, Utime: now},
		{ID: 83, UID: "iam_policy", Name: "IAM策略", ModelGroupID: GROUP_IAM, Category: "iam", Level: 1, Icon: "policy", Provider: "all", Extensible: true, Description: "权限策略", Ctime: now, Utime: now},
	}
}

func getAttributes(now int64) []Attribute {
	attrs := []Attribute{}

	// ========== cloud_vm 虚拟机属性（根据截图提取） ==========
	// 基本信息 (GroupID: 1)
	attrs = append(attrs, Attribute{ID: 1, FieldUID: "cloud_id", FieldName: "云上ID", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "云上ID", Required: true, Display: true, Index: 1, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 2, FieldUID: "instance_id", FieldName: "ID", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "ID", Required: true, Display: true, Index: 2, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 3, FieldUID: "instance_name", FieldName: "名称", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "名称", Required: true, Display: true, Index: 3, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 4, FieldUID: "status", FieldName: "状态", FieldType: "enum", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "状态", Required: true, Display: true, Index: 4, Option: toJSON([]string{"运行中", "已停止", "启动中", "停止中"}), Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 5, FieldUID: "domain", FieldName: "域", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "域", Display: true, Index: 5, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 6, FieldUID: "project", FieldName: "项目", FieldType: "link", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "项目", Display: true, Index: 6, Link: true, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 7, FieldUID: "power_status", FieldName: "电源状态", FieldType: "enum", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "电源状态", Display: true, Index: 7, Option: toJSON([]string{"运行中", "已关机"}), Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 8, FieldUID: "hostname", FieldName: "主机名", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "主机名", Display: true, Index: 8, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 9, FieldUID: "cpu_arch", FieldName: "CPU架构", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "CPU架构", Display: true, Index: 9, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 10, FieldUID: "tags", FieldName: "标签", FieldType: "json", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "标签", Display: true, Index: 10, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 11, FieldUID: "agent_status", FieldName: "Agent安装状态", FieldType: "enum", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "Agent安装状态", Display: true, Index: 11, Option: toJSON([]string{"已安装", "未安装"}), Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 12, FieldUID: "associate_key", FieldName: "关联密钥", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "关联密钥", Display: true, Index: 12, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 13, FieldUID: "platform", FieldName: "平台", FieldType: "enum", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "平台", Required: true, Display: true, Index: 13, Option: toJSON([]string{"aliyun", "aws", "huawei", "tencent", "azure", "volcano"}), Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 14, FieldUID: "charge_type", FieldName: "计费方式", FieldType: "enum", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "计费方式", Display: true, Index: 14, Option: toJSON([]string{"按量付费", "包年包月"}), Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 15, FieldUID: "password", FieldName: "密码", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "密码", Display: false, Index: 15, Secure: true, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 16, FieldUID: "region", FieldName: "区域", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "区域", Required: true, Display: true, Index: 16, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 17, FieldUID: "zone", FieldName: "可用区", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "可用区", Display: true, Index: 17, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 18, FieldUID: "cloud_account_id", FieldName: "云账号", FieldType: "link", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "云账号", Required: true, Display: true, Index: 18, Link: true, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 19, FieldUID: "cloud_subscription_id", FieldName: "云订阅", FieldType: "link", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "云订阅", Display: true, Index: 19, Link: true, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 20, FieldUID: "creation_time", FieldName: "创建时间", FieldType: "datetime", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "创建时间", Display: true, Index: 20, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 21, FieldUID: "update_time", FieldName: "更新时间", FieldType: "datetime", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "更新时间", Display: true, Index: 21, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 22, FieldUID: "remark", FieldName: "备注", FieldType: "text", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "备注", Display: true, Index: 22, Ctime: now, Utime: now})

	// 配置信息 (GroupID: 2)
	attrs = append(attrs, Attribute{ID: 30, FieldUID: "os_type", FieldName: "操作系统", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "操作系统", Display: true, Index: 1, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 31, FieldUID: "private_ip", FieldName: "IP", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "IP", Display: true, Index: 2, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 32, FieldUID: "public_ip", FieldName: "辅助IP", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "辅助IP", Display: true, Index: 3, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 33, FieldUID: "mac_address", FieldName: "MAC", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "MAC", Display: true, Index: 4, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 34, FieldUID: "os_image", FieldName: "系统镜像", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "系统镜像", Display: true, Index: 5, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 35, FieldUID: "hypervisor", FieldName: "宿主机", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "宿主机", Display: true, Index: 6, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 36, FieldUID: "security_group_id", FieldName: "安全组", FieldType: "link", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "安全组", Display: true, Index: 7, Link: true, LinkModel: "security_group", Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 37, FieldUID: "vpc_id", FieldName: "VPC", FieldType: "link", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "VPC", Display: true, Index: 8, Link: true, LinkModel: "vpc", Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 38, FieldUID: "cpu", FieldName: "CPU", FieldType: "int", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "CPU", Display: true, Index: 9, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 39, FieldUID: "memory", FieldName: "内存", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "内存", Display: true, Index: 10, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 40, FieldUID: "system_disk", FieldName: "系统盘", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "系统盘", Display: true, Index: 11, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 41, FieldUID: "data_disk", FieldName: "数据盘", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "数据盘", Display: true, Index: 12, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 42, FieldUID: "iso", FieldName: "ISO", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "ISO", Display: true, Index: 13, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 43, FieldUID: "gpu_device", FieldName: "透传设备", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "透传设备", Display: true, Index: 14, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 44, FieldUID: "usb", FieldName: "USB", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "USB", Display: true, Index: 15, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 45, FieldUID: "auto_start", FieldName: "自动启动", FieldType: "bool", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "自动启动", Display: true, Index: 16, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 46, FieldUID: "max_bandwidth", FieldName: "最大带宽", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "最大带宽", Display: true, Index: 17, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 47, FieldUID: "monitor_url", FieldName: "监控地址", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "监控地址", Display: true, Index: 18, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 48, FieldUID: "boot_mode", FieldName: "引导模式", FieldType: "enum", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "引导模式", Display: true, Index: 19, Option: toJSON([]string{"BIOS", "UEFI"}), Ctime: now, Utime: now})

	// 加密 (GroupID: 5)
	attrs = append(attrs, Attribute{ID: 50, FieldUID: "encryption_key", FieldName: "加密密钥", FieldType: "string", ModelUID: "cloud_vm", GroupID: 5, DisplayName: "加密密钥", Display: true, Index: 1, Secure: true, Ctime: now, Utime: now})

	// ========== 通用属性（所有模型共用） ==========
	// 云厂商字段 - 用于区分资源来源
	commonProviderAttr := func(id int64, modelUID string, groupID int64) Attribute {
		return Attribute{ID: id, FieldUID: "provider", FieldName: "云厂商", FieldType: "enum", ModelUID: modelUID, GroupID: groupID, Required: true, Display: true, Index: 1, Option: toJSON([]string{"aliyun", "aws", "tencent", "huawei", "volcano", "azure", "gcp"}), Ctime: now, Utime: now}
	}
	commonRegionAttr := func(id int64, modelUID string, groupID int64, index int) Attribute {
		return Attribute{ID: id, FieldUID: "region", FieldName: "地域", FieldType: "string", ModelUID: modelUID, GroupID: groupID, Required: true, Display: true, Index: index, Ctime: now, Utime: now}
	}
	commonAccountAttr := func(id int64, modelUID string, groupID int64, index int) Attribute {
		return Attribute{ID: id, FieldUID: "cloud_account_id", FieldName: "云账号", FieldType: "string", ModelUID: modelUID, GroupID: groupID, Required: true, Display: true, Index: index, Ctime: now, Utime: now}
	}

	// ========== 主机属性 ==========
	attrs = append(attrs, Attribute{ID: 61, FieldUID: "hostname", FieldName: "主机名", FieldType: "string", ModelUID: "host", GroupID: 40, Required: true, Display: true, Index: 1, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 62, FieldUID: "ip_address", FieldName: "IP地址", FieldType: "string", ModelUID: "host", GroupID: 40, Required: true, Display: true, Index: 2, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 63, FieldUID: "os_type", FieldName: "操作系统", FieldType: "enum", ModelUID: "host", GroupID: 40, Required: false, Display: true, Index: 3, Option: toJSON([]string{"Linux", "Windows", "macOS"}), Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 64, FieldUID: "cpu", FieldName: "CPU核数", FieldType: "int", ModelUID: "host", GroupID: 41, Required: false, Display: true, Index: 1, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 65, FieldUID: "memory", FieldName: "内存(GB)", FieldType: "int", ModelUID: "host", GroupID: 41, Required: false, Display: true, Index: 2, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 66, FieldUID: "status", FieldName: "状态", FieldType: "enum", ModelUID: "host", GroupID: 40, Required: true, Display: true, Index: 4, Option: toJSON([]string{"running", "stopped", "unknown"}), Ctime: now, Utime: now})

	// ========== 云服务器(ECS)属性 ==========
	attrs = append(attrs, commonProviderAttr(100, "ecs", 10))
	attrs = append(attrs, commonAccountAttr(101, "ecs", 10, 2))
	attrs = append(attrs, commonRegionAttr(102, "ecs", 10, 3))
	attrs = append(attrs, Attribute{ID: 103, FieldUID: "instance_id", FieldName: "实例ID", FieldType: "string", ModelUID: "ecs", GroupID: 10, Required: true, Display: true, Index: 4, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 104, FieldUID: "instance_name", FieldName: "实例名称", FieldType: "string", ModelUID: "ecs", GroupID: 10, Required: true, Display: true, Index: 5, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 105, FieldUID: "status", FieldName: "状态", FieldType: "enum", ModelUID: "ecs", GroupID: 10, Required: true, Display: true, Index: 6, Option: toJSON([]string{"Running", "Stopped", "Starting", "Stopping", "Pending"}), Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 106, FieldUID: "instance_type", FieldName: "实例规格", FieldType: "string", ModelUID: "ecs", GroupID: 11, Required: true, Display: true, Index: 1, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 107, FieldUID: "cpu", FieldName: "CPU核数", FieldType: "int", ModelUID: "ecs", GroupID: 11, Required: false, Display: true, Index: 2, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 108, FieldUID: "memory", FieldName: "内存(GB)", FieldType: "int", ModelUID: "ecs", GroupID: 11, Required: false, Display: true, Index: 3, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 109, FieldUID: "os_type", FieldName: "操作系统", FieldType: "string", ModelUID: "ecs", GroupID: 11, Required: false, Display: true, Index: 4, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 110, FieldUID: "private_ip", FieldName: "私网IP", FieldType: "string", ModelUID: "ecs", GroupID: 12, Required: false, Display: true, Index: 1, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 111, FieldUID: "public_ip", FieldName: "公网IP", FieldType: "string", ModelUID: "ecs", GroupID: 12, Required: false, Display: true, Index: 2, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 112, FieldUID: "vpc_id", FieldName: "VPC ID", FieldType: "string", ModelUID: "ecs", GroupID: 12, Required: false, Display: true, Index: 3, Link: true, LinkModel: "vpc", Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 113, FieldUID: "subnet_id", FieldName: "子网ID", FieldType: "string", ModelUID: "ecs", GroupID: 12, Required: false, Display: true, Index: 4, Link: true, LinkModel: "subnet", Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 114, FieldUID: "zone", FieldName: "可用区", FieldType: "string", ModelUID: "ecs", GroupID: 10, Required: false, Display: true, Index: 7, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 115, FieldUID: "create_time", FieldName: "创建时间", FieldType: "datetime", ModelUID: "ecs", GroupID: 10, Required: false, Display: true, Index: 8, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 116, FieldUID: "expire_time", FieldName: "到期时间", FieldType: "datetime", ModelUID: "ecs", GroupID: 10, Required: false, Display: true, Index: 9, Ctime: now, Utime: now})

	// ========== VPC属性 ==========
	attrs = append(attrs, commonProviderAttr(200, "vpc", 20))
	attrs = append(attrs, commonAccountAttr(201, "vpc", 20, 2))
	attrs = append(attrs, commonRegionAttr(202, "vpc", 20, 3))
	attrs = append(attrs, Attribute{ID: 203, FieldUID: "vpc_id", FieldName: "VPC ID", FieldType: "string", ModelUID: "vpc", GroupID: 20, Required: true, Display: true, Index: 4, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 204, FieldUID: "vpc_name", FieldName: "VPC名称", FieldType: "string", ModelUID: "vpc", GroupID: 20, Required: true, Display: true, Index: 5, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 205, FieldUID: "cidr_block", FieldName: "CIDR", FieldType: "string", ModelUID: "vpc", GroupID: 20, Required: true, Display: true, Index: 6, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 206, FieldUID: "status", FieldName: "状态", FieldType: "enum", ModelUID: "vpc", GroupID: 20, Required: true, Display: true, Index: 7, Option: toJSON([]string{"Available", "Pending", "Deleting"}), Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 207, FieldUID: "description", FieldName: "描述", FieldType: "string", ModelUID: "vpc", GroupID: 20, Required: false, Display: true, Index: 8, Ctime: now, Utime: now})

	// ========== 子网属性 ==========
	attrs = append(attrs, commonProviderAttr(220, "subnet", 80))
	attrs = append(attrs, commonAccountAttr(221, "subnet", 80, 2))
	attrs = append(attrs, commonRegionAttr(222, "subnet", 80, 3))
	attrs = append(attrs, Attribute{ID: 223, FieldUID: "subnet_id", FieldName: "子网ID", FieldType: "string", ModelUID: "subnet", GroupID: 80, Required: true, Display: true, Index: 4, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 224, FieldUID: "subnet_name", FieldName: "子网名称", FieldType: "string", ModelUID: "subnet", GroupID: 80, Required: true, Display: true, Index: 5, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 225, FieldUID: "vpc_id", FieldName: "VPC ID", FieldType: "string", ModelUID: "subnet", GroupID: 80, Required: true, Display: true, Index: 6, Link: true, LinkModel: "vpc", Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 226, FieldUID: "cidr_block", FieldName: "CIDR", FieldType: "string", ModelUID: "subnet", GroupID: 80, Required: true, Display: true, Index: 7, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 227, FieldUID: "zone", FieldName: "可用区", FieldType: "string", ModelUID: "subnet", GroupID: 80, Required: false, Display: true, Index: 8, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 228, FieldUID: "status", FieldName: "状态", FieldType: "enum", ModelUID: "subnet", GroupID: 80, Required: true, Display: true, Index: 9, Option: toJSON([]string{"Available", "Pending"}), Ctime: now, Utime: now})

	// ========== RDS属性 ==========
	attrs = append(attrs, commonProviderAttr(300, "rds", 30))
	attrs = append(attrs, commonAccountAttr(301, "rds", 30, 2))
	attrs = append(attrs, commonRegionAttr(302, "rds", 30, 3))
	attrs = append(attrs, Attribute{ID: 303, FieldUID: "instance_id", FieldName: "实例ID", FieldType: "string", ModelUID: "rds", GroupID: 30, Required: true, Display: true, Index: 4, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 304, FieldUID: "instance_name", FieldName: "实例名称", FieldType: "string", ModelUID: "rds", GroupID: 30, Required: false, Display: true, Index: 5, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 305, FieldUID: "engine", FieldName: "数据库引擎", FieldType: "enum", ModelUID: "rds", GroupID: 31, Required: true, Display: true, Index: 1, Option: toJSON([]string{"MySQL", "PostgreSQL", "SQLServer", "MariaDB", "Oracle"}), Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 306, FieldUID: "engine_version", FieldName: "引擎版本", FieldType: "string", ModelUID: "rds", GroupID: 31, Required: true, Display: true, Index: 2, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 307, FieldUID: "instance_class", FieldName: "实例规格", FieldType: "string", ModelUID: "rds", GroupID: 31, Required: true, Display: true, Index: 3, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 308, FieldUID: "storage", FieldName: "存储空间(GB)", FieldType: "int", ModelUID: "rds", GroupID: 31, Required: false, Display: true, Index: 4, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 309, FieldUID: "status", FieldName: "状态", FieldType: "enum", ModelUID: "rds", GroupID: 30, Required: true, Display: true, Index: 6, Option: toJSON([]string{"Running", "Creating", "Deleting", "Restarting", "Stopped"}), Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 310, FieldUID: "connection_string", FieldName: "连接地址", FieldType: "string", ModelUID: "rds", GroupID: 31, Required: false, Display: true, Index: 5, Secure: true, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 311, FieldUID: "port", FieldName: "端口", FieldType: "int", ModelUID: "rds", GroupID: 31, Required: false, Display: true, Index: 6, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 312, FieldUID: "vpc_id", FieldName: "VPC ID", FieldType: "string", ModelUID: "rds", GroupID: 31, Required: false, Display: true, Index: 7, Link: true, LinkModel: "vpc", Ctime: now, Utime: now})

	// ========== 安全组属性 ==========
	attrs = append(attrs, commonProviderAttr(400, "security_group", 70))
	attrs = append(attrs, commonAccountAttr(401, "security_group", 70, 2))
	attrs = append(attrs, commonRegionAttr(402, "security_group", 70, 3))
	attrs = append(attrs, Attribute{ID: 403, FieldUID: "security_group_id", FieldName: "安全组ID", FieldType: "string", ModelUID: "security_group", GroupID: 70, Required: true, Display: true, Index: 4, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 404, FieldUID: "security_group_name", FieldName: "安全组名称", FieldType: "string", ModelUID: "security_group", GroupID: 70, Required: true, Display: true, Index: 5, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 405, FieldUID: "vpc_id", FieldName: "VPC ID", FieldType: "string", ModelUID: "security_group", GroupID: 70, Required: false, Display: true, Index: 6, Link: true, LinkModel: "vpc", Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 406, FieldUID: "description", FieldName: "描述", FieldType: "string", ModelUID: "security_group", GroupID: 70, Required: false, Display: true, Index: 7, Ctime: now, Utime: now})

	// ========== IAM用户属性 ==========
	attrs = append(attrs, commonProviderAttr(500, "iam_user", 50))
	attrs = append(attrs, commonAccountAttr(501, "iam_user", 50, 2))
	attrs = append(attrs, Attribute{ID: 502, FieldUID: "user_id", FieldName: "用户ID", FieldType: "string", ModelUID: "iam_user", GroupID: 50, Required: true, Display: true, Index: 3, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 503, FieldUID: "user_name", FieldName: "用户名", FieldType: "string", ModelUID: "iam_user", GroupID: 50, Required: true, Display: true, Index: 4, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 504, FieldUID: "display_name", FieldName: "显示名称", FieldType: "string", ModelUID: "iam_user", GroupID: 50, Required: false, Display: true, Index: 5, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 505, FieldUID: "email", FieldName: "邮箱", FieldType: "string", ModelUID: "iam_user", GroupID: 51, Required: false, Display: true, Index: 1, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 506, FieldUID: "phone", FieldName: "手机号", FieldType: "string", ModelUID: "iam_user", GroupID: 51, Required: false, Display: true, Index: 2, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 507, FieldUID: "create_time", FieldName: "创建时间", FieldType: "datetime", ModelUID: "iam_user", GroupID: 50, Required: false, Display: true, Index: 6, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 508, FieldUID: "last_login_time", FieldName: "最后登录时间", FieldType: "datetime", ModelUID: "iam_user", GroupID: 50, Required: false, Display: true, Index: 7, Ctime: now, Utime: now})

	// ========== K8s集群属性 ==========
	attrs = append(attrs, commonProviderAttr(600, "k8s_cluster", 60))
	attrs = append(attrs, commonAccountAttr(601, "k8s_cluster", 60, 2))
	attrs = append(attrs, commonRegionAttr(602, "k8s_cluster", 60, 3))
	attrs = append(attrs, Attribute{ID: 603, FieldUID: "cluster_id", FieldName: "集群ID", FieldType: "string", ModelUID: "k8s_cluster", GroupID: 60, Required: true, Display: true, Index: 4, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 604, FieldUID: "cluster_name", FieldName: "集群名称", FieldType: "string", ModelUID: "k8s_cluster", GroupID: 60, Required: true, Display: true, Index: 5, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 605, FieldUID: "version", FieldName: "K8s版本", FieldType: "string", ModelUID: "k8s_cluster", GroupID: 61, Required: false, Display: true, Index: 1, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 606, FieldUID: "status", FieldName: "状态", FieldType: "enum", ModelUID: "k8s_cluster", GroupID: 60, Required: true, Display: true, Index: 6, Option: toJSON([]string{"Running", "Creating", "Deleting", "Updating", "Failed"}), Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 607, FieldUID: "node_count", FieldName: "节点数", FieldType: "int", ModelUID: "k8s_cluster", GroupID: 61, Required: false, Display: true, Index: 2, Ctime: now, Utime: now})
	attrs = append(attrs, Attribute{ID: 608, FieldUID: "vpc_id", FieldName: "VPC ID", FieldType: "string", ModelUID: "k8s_cluster", GroupID: 61, Required: false, Display: true, Index: 3, Link: true, LinkModel: "vpc", Ctime: now, Utime: now})

	return attrs
}

// getAttributeGroups 获取属性分组
func getAttributeGroups(now int64) []AttributeGroup {
	return []AttributeGroup{
		// ========== cloud_vm 虚拟机属性分组 ==========
		{ID: 1, UID: "basic", Name: "基本信息", ModelUID: "cloud_vm", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 2, UID: "config", Name: "配置信息", ModelUID: "cloud_vm", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 3, UID: "network", Name: "网络信息", ModelUID: "cloud_vm", Index: 3, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 4, UID: "storage", Name: "存储信息", ModelUID: "cloud_vm", Index: 4, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 5, UID: "security", Name: "加密", ModelUID: "cloud_vm", Index: 5, IsBuiltin: true, Ctime: now, Utime: now},

		// ========== ecs 云服务器属性分组 ==========
		{ID: 10, UID: "basic", Name: "基本信息", ModelUID: "ecs", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 11, UID: "config", Name: "配置信息", ModelUID: "ecs", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 12, UID: "network", Name: "网络信息", ModelUID: "ecs", Index: 3, IsBuiltin: true, Ctime: now, Utime: now},

		// ========== vpc 属性分组 ==========
		{ID: 20, UID: "basic", Name: "基本信息", ModelUID: "vpc", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},

		// ========== rds 属性分组 ==========
		{ID: 30, UID: "basic", Name: "基本信息", ModelUID: "rds", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 31, UID: "config", Name: "配置信息", ModelUID: "rds", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},

		// ========== host 属性分组 ==========
		{ID: 40, UID: "basic", Name: "基本信息", ModelUID: "host", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 41, UID: "config", Name: "配置信息", ModelUID: "host", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},

		// ========== iam_user 属性分组 ==========
		{ID: 50, UID: "basic", Name: "基本信息", ModelUID: "iam_user", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 51, UID: "contact", Name: "联系方式", ModelUID: "iam_user", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},

		// ========== k8s_cluster 属性分组 ==========
		{ID: 60, UID: "basic", Name: "基本信息", ModelUID: "k8s_cluster", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 61, UID: "config", Name: "配置信息", ModelUID: "k8s_cluster", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},

		// ========== security_group 属性分组 ==========
		{ID: 70, UID: "basic", Name: "基本信息", ModelUID: "security_group", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},

		// ========== subnet 属性分组 ==========
		{ID: 80, UID: "basic", Name: "基本信息", ModelUID: "subnet", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
	}
}

func getModelRelations(now int64) []ModelRelation {
	return []ModelRelation{
		// ========== 网络关系 ==========
		{ID: 1, UID: "vpc_subnet", Name: "VPC包含子网", SourceModelUID: "vpc", TargetModelUID: "subnet", RelationType: "contains", Direction: "one_to_many", Description: "VPC包含的子网", Ctime: now, Utime: now},
		{ID: 2, UID: "vpc_route_table", Name: "VPC包含路由表", SourceModelUID: "vpc", TargetModelUID: "route_table", RelationType: "contains", Direction: "one_to_many", Description: "VPC包含的路由表", Ctime: now, Utime: now},

		// ========== 计算资源关系 ==========
		{ID: 10, UID: "ecs_vpc", Name: "云服务器所属VPC", SourceModelUID: "ecs", TargetModelUID: "vpc", RelationType: "belongs_to", Direction: "many_to_one", Description: "云服务器所属的VPC", Ctime: now, Utime: now},
		{ID: 11, UID: "ecs_subnet", Name: "云服务器所属子网", SourceModelUID: "ecs", TargetModelUID: "subnet", RelationType: "belongs_to", Direction: "many_to_one", Description: "云服务器所属的子网", Ctime: now, Utime: now},
		{ID: 12, UID: "ecs_security_group", Name: "云服务器绑定安全组", SourceModelUID: "ecs", TargetModelUID: "security_group", RelationType: "bindto", Direction: "many_to_many", Description: "云服务器绑定的安全组", Ctime: now, Utime: now},
		{ID: 13, UID: "ecs_disk", Name: "云服务器挂载云盘", SourceModelUID: "ecs", TargetModelUID: "disk", RelationType: "bindto", Direction: "one_to_many", Description: "云服务器挂载的云盘", Ctime: now, Utime: now},
		{ID: 14, UID: "eip_ecs", Name: "弹性IP绑定云服务器", SourceModelUID: "eip", TargetModelUID: "ecs", RelationType: "bindto", Direction: "one_to_one", Description: "弹性IP绑定的云服务器", Ctime: now, Utime: now},

		// ========== 负载均衡关系 ==========
		{ID: 20, UID: "lb_vpc", Name: "负载均衡所属VPC", SourceModelUID: "lb", TargetModelUID: "vpc", RelationType: "belongs_to", Direction: "many_to_one", Description: "负载均衡所属的VPC", Ctime: now, Utime: now},
		{ID: 21, UID: "lb_ecs", Name: "负载均衡后端服务器", SourceModelUID: "lb", TargetModelUID: "ecs", RelationType: "connects", Direction: "one_to_many", Description: "负载均衡后端的云服务器", Ctime: now, Utime: now},

		// ========== 数据库关系 ==========
		{ID: 30, UID: "rds_vpc", Name: "数据库所属VPC", SourceModelUID: "rds", TargetModelUID: "vpc", RelationType: "belongs_to", Direction: "many_to_one", Description: "数据库所属的VPC", Ctime: now, Utime: now},
		{ID: 31, UID: "redis_vpc", Name: "Redis所属VPC", SourceModelUID: "redis", TargetModelUID: "vpc", RelationType: "belongs_to", Direction: "many_to_one", Description: "Redis所属的VPC", Ctime: now, Utime: now},
		{ID: 32, UID: "mongodb_vpc", Name: "MongoDB所属VPC", SourceModelUID: "mongodb", TargetModelUID: "vpc", RelationType: "belongs_to", Direction: "many_to_one", Description: "MongoDB所属的VPC", Ctime: now, Utime: now},

		// ========== 容器关系 ==========
		{ID: 40, UID: "k8s_cluster_vpc", Name: "K8s集群所属VPC", SourceModelUID: "k8s_cluster", TargetModelUID: "vpc", RelationType: "belongs_to", Direction: "many_to_one", Description: "K8s集群所属的VPC", Ctime: now, Utime: now},
		{ID: 41, UID: "k8s_cluster_namespace", Name: "K8s集群包含命名空间", SourceModelUID: "k8s_cluster", TargetModelUID: "k8s_namespace", RelationType: "contains", Direction: "one_to_many", Description: "K8s集群包含的命名空间", Ctime: now, Utime: now},
		{ID: 42, UID: "k8s_cluster_node", Name: "K8s集群包含节点", SourceModelUID: "k8s_cluster", TargetModelUID: "k8s_node", RelationType: "contains", Direction: "one_to_many", Description: "K8s集群包含的节点", Ctime: now, Utime: now},
		{ID: 43, UID: "k8s_namespace_deployment", Name: "命名空间包含Deployment", SourceModelUID: "k8s_namespace", TargetModelUID: "k8s_deployment", RelationType: "contains", Direction: "one_to_many", Description: "命名空间包含的Deployment", Ctime: now, Utime: now},
		{ID: 44, UID: "k8s_namespace_service", Name: "命名空间包含Service", SourceModelUID: "k8s_namespace", TargetModelUID: "k8s_service", RelationType: "contains", Direction: "one_to_many", Description: "命名空间包含的Service", Ctime: now, Utime: now},
		{ID: 45, UID: "k8s_namespace_pod", Name: "命名空间包含Pod", SourceModelUID: "k8s_namespace", TargetModelUID: "k8s_pod", RelationType: "contains", Direction: "one_to_many", Description: "命名空间包含的Pod", Ctime: now, Utime: now},
		{ID: 46, UID: "k8s_deployment_pod", Name: "Deployment管理Pod", SourceModelUID: "k8s_deployment", TargetModelUID: "k8s_pod", RelationType: "contains", Direction: "one_to_many", Description: "Deployment管理的Pod", Ctime: now, Utime: now},
		{ID: 47, UID: "k8s_service_pod", Name: "Service关联Pod", SourceModelUID: "k8s_service", TargetModelUID: "k8s_pod", RelationType: "connects", Direction: "one_to_many", Description: "Service关联的Pod", Ctime: now, Utime: now},
		{ID: 48, UID: "k8s_pod_node", Name: "Pod运行在Node", SourceModelUID: "k8s_pod", TargetModelUID: "k8s_node", RelationType: "belongs_to", Direction: "many_to_one", Description: "Pod运行的Node", Ctime: now, Utime: now},

		// ========== IAM关系 ==========
		{ID: 50, UID: "iam_user_group", Name: "用户所属用户组", SourceModelUID: "iam_user", TargetModelUID: "iam_group", RelationType: "belongs_to", Direction: "many_to_many", Description: "用户所属的用户组", Ctime: now, Utime: now},
		{ID: 51, UID: "iam_user_policy", Name: "用户绑定策略", SourceModelUID: "iam_user", TargetModelUID: "iam_policy", RelationType: "bindto", Direction: "many_to_many", Description: "用户绑定的策略", Ctime: now, Utime: now},
		{ID: 52, UID: "iam_group_policy", Name: "用户组绑定策略", SourceModelUID: "iam_group", TargetModelUID: "iam_policy", RelationType: "bindto", Direction: "many_to_many", Description: "用户组绑定的策略", Ctime: now, Utime: now},
		{ID: 53, UID: "iam_role_policy", Name: "角色绑定策略", SourceModelUID: "iam_role", TargetModelUID: "iam_policy", RelationType: "bindto", Direction: "many_to_many", Description: "角色绑定的策略", Ctime: now, Utime: now},

		// ========== 安全组关系 ==========
		{ID: 60, UID: "security_group_vpc", Name: "安全组所属VPC", SourceModelUID: "security_group", TargetModelUID: "vpc", RelationType: "belongs_to", Direction: "many_to_one", Description: "安全组所属的VPC", Ctime: now, Utime: now},

		// ========== NAT网关关系 ==========
		{ID: 70, UID: "nat_vpc", Name: "NAT网关所属VPC", SourceModelUID: "nat", TargetModelUID: "vpc", RelationType: "belongs_to", Direction: "many_to_one", Description: "NAT网关所属的VPC", Ctime: now, Utime: now},
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

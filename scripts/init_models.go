//go:build ignore
// +build ignore

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
	Provider     string `bson:"provider"`
	Extensible   bool   `bson:"extensible"`
	Ctime        int64  `bson:"ctime"`
	Utime        int64  `bson:"utime"`
}

// ModelField 字段定义
type ModelField struct {
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

// ModelGroup 模型分组
type ModelGroup struct {
	ID    int64  `bson:"id"`
	Name  string `bson:"name"`
	Ctime int64  `bson:"ctime"`
	Utime int64  `bson:"utime"`
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

// RelationType 关系类型定义
type RelationType struct {
	ID             int64  `bson:"id"`
	UID            string `bson:"uid"`
	Name           string `bson:"name"`
	SourceModelUID string `bson:"source_model_uid"`
	TargetModelUID string `bson:"target_model_uid"`
	Direction      string `bson:"direction"`
	Description    string `bson:"description"`
	Ctime          int64  `bson:"ctime"`
	Utime          int64  `bson:"utime"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	now := time.Now().UnixMilli()

	// 初始化模型分组
	groups := []ModelGroup{
		{ID: 1, Name: "计算", Ctime: now, Utime: now},
		{ID: 2, Name: "存储", Ctime: now, Utime: now},
		{ID: 3, Name: "网络", Ctime: now, Utime: now},
		{ID: 4, Name: "数据库", Ctime: now, Utime: now},
		{ID: 5, Name: "安全", Ctime: now, Utime: now},
		{ID: 6, Name: "身份管理", Ctime: now, Utime: now},
	}

	// 初始化模型
	models := []Model{
		// 通用云资源模型 (用于跨云查询)
		{ID: 1, UID: "cloud_vm", Name: "虚拟机", ModelGroupID: 1, Category: "compute", Level: 1, Provider: "all", Icon: "server", Description: "云服务器/虚拟机", Extensible: true, Ctime: now, Utime: now},
		{ID: 2, UID: "cloud_rds", Name: "关系型数据库", ModelGroupID: 4, Category: "database", Level: 1, Provider: "all", Icon: "database", Description: "云关系型数据库", Extensible: true, Ctime: now, Utime: now},
		{ID: 3, UID: "cloud_redis", Name: "Redis缓存", ModelGroupID: 4, Category: "database", Level: 1, Provider: "all", Icon: "cache", Description: "云Redis缓存", Extensible: true, Ctime: now, Utime: now},
		{ID: 4, UID: "cloud_mongodb", Name: "MongoDB", ModelGroupID: 4, Category: "database", Level: 1, Provider: "all", Icon: "database", Description: "云MongoDB数据库", Extensible: true, Ctime: now, Utime: now},
		{ID: 5, UID: "cloud_vpc", Name: "VPC", ModelGroupID: 3, Category: "network", Level: 1, Provider: "all", Icon: "network", Description: "虚拟私有云", Extensible: true, Ctime: now, Utime: now},
		{ID: 6, UID: "cloud_eip", Name: "弹性公网IP", ModelGroupID: 3, Category: "network", Level: 1, Provider: "all", Icon: "ip", Description: "弹性公网IP", Extensible: true, Ctime: now, Utime: now},
		{ID: 7, UID: "cloud_slb", Name: "负载均衡", ModelGroupID: 3, Category: "network", Level: 1, Provider: "all", Icon: "loadbalancer", Description: "云负载均衡", Extensible: true, Ctime: now, Utime: now},
		{ID: 8, UID: "cloud_oss", Name: "对象存储", ModelGroupID: 2, Category: "storage", Level: 1, Provider: "all", Icon: "storage", Description: "云对象存储", Extensible: true, Ctime: now, Utime: now},

		// 阿里云
		{ID: 11, UID: "aliyun_ecs", Name: "阿里云ECS", ModelGroupID: 1, ParentUID: "cloud_vm", Category: "compute", Level: 2, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 12, UID: "aliyun_rds", Name: "阿里云RDS", ModelGroupID: 4, ParentUID: "cloud_rds", Category: "database", Level: 2, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 13, UID: "aliyun_redis", Name: "阿里云Redis", ModelGroupID: 4, ParentUID: "cloud_redis", Category: "database", Level: 2, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 14, UID: "aliyun_mongodb", Name: "阿里云MongoDB", ModelGroupID: 4, ParentUID: "cloud_mongodb", Category: "database", Level: 2, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 15, UID: "aliyun_vpc", Name: "阿里云VPC", ModelGroupID: 3, ParentUID: "cloud_vpc", Category: "network", Level: 2, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 16, UID: "aliyun_eip", Name: "阿里云EIP", ModelGroupID: 3, ParentUID: "cloud_eip", Category: "network", Level: 2, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 17, UID: "aliyun_slb", Name: "阿里云SLB", ModelGroupID: 3, ParentUID: "cloud_slb", Category: "network", Level: 2, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 18, UID: "aliyun_oss", Name: "阿里云OSS", ModelGroupID: 2, ParentUID: "cloud_oss", Category: "storage", Level: 2, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 19, UID: "aliyun_security_group", Name: "阿里云安全组", ModelGroupID: 5, Category: "security", Level: 1, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 20, UID: "aliyun_ram_user", Name: "阿里云RAM用户", ModelGroupID: 6, Category: "iam", Level: 1, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 21, UID: "aliyun_ram_group", Name: "阿里云RAM用户组", ModelGroupID: 6, Category: "iam", Level: 1, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 22, UID: "aliyun_ram_policy", Name: "阿里云RAM策略", ModelGroupID: 6, Category: "iam", Level: 1, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},

		// AWS
		{ID: 101, UID: "aws_ecs", Name: "AWS EC2", ModelGroupID: 1, ParentUID: "cloud_vm", Category: "compute", Level: 2, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 102, UID: "aws_rds", Name: "AWS RDS", ModelGroupID: 4, ParentUID: "cloud_rds", Category: "database", Level: 2, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 103, UID: "aws_redis", Name: "AWS ElastiCache", ModelGroupID: 4, ParentUID: "cloud_redis", Category: "database", Level: 2, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 104, UID: "aws_mongodb", Name: "AWS DocumentDB", ModelGroupID: 4, ParentUID: "cloud_mongodb", Category: "database", Level: 2, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 105, UID: "aws_vpc", Name: "AWS VPC", ModelGroupID: 3, ParentUID: "cloud_vpc", Category: "network", Level: 2, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 106, UID: "aws_eip", Name: "AWS Elastic IP", ModelGroupID: 3, ParentUID: "cloud_eip", Category: "network", Level: 2, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 107, UID: "aws_s3", Name: "AWS S3", ModelGroupID: 2, ParentUID: "cloud_oss", Category: "storage", Level: 2, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 108, UID: "aws_iam_user", Name: "AWS IAM用户", ModelGroupID: 6, Category: "iam", Level: 1, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 109, UID: "aws_iam_group", Name: "AWS IAM用户组", ModelGroupID: 6, Category: "iam", Level: 1, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 110, UID: "aws_iam_policy", Name: "AWS IAM策略", ModelGroupID: 6, Category: "iam", Level: 1, Provider: "aws", Extensible: true, Ctime: now, Utime: now},

		// 华为云
		{ID: 201, UID: "huawei_ecs", Name: "华为云ECS", ModelGroupID: 1, ParentUID: "cloud_vm", Category: "compute", Level: 2, Provider: "huawei", Extensible: true, Ctime: now, Utime: now},
		{ID: 202, UID: "huawei_rds", Name: "华为云RDS", ModelGroupID: 4, ParentUID: "cloud_rds", Category: "database", Level: 2, Provider: "huawei", Extensible: true, Ctime: now, Utime: now},
		{ID: 203, UID: "huawei_redis", Name: "华为云DCS", ModelGroupID: 4, ParentUID: "cloud_redis", Category: "database", Level: 2, Provider: "huawei", Extensible: true, Ctime: now, Utime: now},
		{ID: 204, UID: "huawei_mongodb", Name: "华为云DDS", ModelGroupID: 4, ParentUID: "cloud_mongodb", Category: "database", Level: 2, Provider: "huawei", Extensible: true, Ctime: now, Utime: now},
		{ID: 205, UID: "huawei_vpc", Name: "华为云VPC", ModelGroupID: 3, ParentUID: "cloud_vpc", Category: "network", Level: 2, Provider: "huawei", Extensible: true, Ctime: now, Utime: now},
		{ID: 206, UID: "huawei_eip", Name: "华为云EIP", ModelGroupID: 3, ParentUID: "cloud_eip", Category: "network", Level: 2, Provider: "huawei", Extensible: true, Ctime: now, Utime: now},

		// 腾讯云
		{ID: 301, UID: "tencent_ecs", Name: "腾讯云CVM", ModelGroupID: 1, ParentUID: "cloud_vm", Category: "compute", Level: 2, Provider: "tencent", Extensible: true, Ctime: now, Utime: now},
		{ID: 302, UID: "tencent_rds", Name: "腾讯云CDB", ModelGroupID: 4, ParentUID: "cloud_rds", Category: "database", Level: 2, Provider: "tencent", Extensible: true, Ctime: now, Utime: now},
		{ID: 303, UID: "tencent_redis", Name: "腾讯云Redis", ModelGroupID: 4, ParentUID: "cloud_redis", Category: "database", Level: 2, Provider: "tencent", Extensible: true, Ctime: now, Utime: now},
		{ID: 304, UID: "tencent_mongodb", Name: "腾讯云MongoDB", ModelGroupID: 4, ParentUID: "cloud_mongodb", Category: "database", Level: 2, Provider: "tencent", Extensible: true, Ctime: now, Utime: now},
		{ID: 305, UID: "tencent_vpc", Name: "腾讯云VPC", ModelGroupID: 3, ParentUID: "cloud_vpc", Category: "network", Level: 2, Provider: "tencent", Extensible: true, Ctime: now, Utime: now},
		{ID: 306, UID: "tencent_eip", Name: "腾讯云EIP", ModelGroupID: 3, ParentUID: "cloud_eip", Category: "network", Level: 2, Provider: "tencent", Extensible: true, Ctime: now, Utime: now},

		// 火山引擎
		{ID: 401, UID: "volcano_ecs", Name: "火山引擎ECS", ModelGroupID: 1, ParentUID: "cloud_vm", Category: "compute", Level: 2, Provider: "volcano", Extensible: true, Ctime: now, Utime: now},
		{ID: 402, UID: "volcano_rds", Name: "火山引擎RDS", ModelGroupID: 4, ParentUID: "cloud_rds", Category: "database", Level: 2, Provider: "volcano", Extensible: true, Ctime: now, Utime: now},
		{ID: 403, UID: "volcano_redis", Name: "火山引擎Redis", ModelGroupID: 4, ParentUID: "cloud_redis", Category: "database", Level: 2, Provider: "volcano", Extensible: true, Ctime: now, Utime: now},
		{ID: 404, UID: "volcano_mongodb", Name: "火山引擎MongoDB", ModelGroupID: 4, ParentUID: "cloud_mongodb", Category: "database", Level: 2, Provider: "volcano", Extensible: true, Ctime: now, Utime: now},
		{ID: 405, UID: "volcano_vpc", Name: "火山引擎VPC", ModelGroupID: 3, ParentUID: "cloud_vpc", Category: "network", Level: 2, Provider: "volcano", Extensible: true, Ctime: now, Utime: now},
		{ID: 406, UID: "volcano_eip", Name: "火山引擎EIP", ModelGroupID: 3, ParentUID: "cloud_eip", Category: "network", Level: 2, Provider: "volcano", Extensible: true, Ctime: now, Utime: now},
	}

	// 属性分组定义
	// 通用虚拟机属性分组 (cloud_vm)
	vmAttrGroups := []AttributeGroup{
		{ID: 1, UID: "basic", Name: "基本信息", ModelUID: "cloud_vm", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 2, UID: "config", Name: "配置信息", ModelUID: "cloud_vm", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 3, UID: "network", Name: "网络信息", ModelUID: "cloud_vm", Index: 3, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 4, UID: "storage", Name: "存储信息", ModelUID: "cloud_vm", Index: 4, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 5, UID: "security", Name: "加密", ModelUID: "cloud_vm", Index: 5, IsBuiltin: true, Ctime: now, Utime: now},
	}

	// 通用虚拟机字段 (cloud_vm) - 根据截图提取
	vmFields := []ModelField{
		// 基本信息
		{ID: 1, FieldUID: "cloud_id", FieldName: "云上ID", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "云上ID", Display: true, Index: 1, Required: true, Ctime: now, Utime: now},
		{ID: 2, FieldUID: "instance_id", FieldName: "ID", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "ID", Display: true, Index: 2, Required: true, Ctime: now, Utime: now},
		{ID: 3, FieldUID: "instance_name", FieldName: "名称", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "名称", Display: true, Index: 3, Required: true, Ctime: now, Utime: now},
		{ID: 4, FieldUID: "status", FieldName: "状态", FieldType: "enum", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "状态", Display: true, Index: 4, Required: true, Option: toJSON([]string{"运行中", "已停止", "启动中", "停止中"}), Ctime: now, Utime: now},
		{ID: 5, FieldUID: "domain", FieldName: "域", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "域", Display: true, Index: 5, Ctime: now, Utime: now},
		{ID: 6, FieldUID: "project", FieldName: "项目", FieldType: "link", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "项目", Display: true, Index: 6, Link: true, Ctime: now, Utime: now},
		{ID: 7, FieldUID: "power_status", FieldName: "电源状态", FieldType: "enum", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "电源状态", Display: true, Index: 7, Option: toJSON([]string{"运行中", "已关机"}), Ctime: now, Utime: now},
		{ID: 8, FieldUID: "hostname", FieldName: "主机名", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "主机名", Display: true, Index: 8, Ctime: now, Utime: now},
		{ID: 9, FieldUID: "cpu_arch", FieldName: "CPU架构", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "CPU架构", Display: true, Index: 9, Ctime: now, Utime: now},
		{ID: 10, FieldUID: "tags", FieldName: "标签", FieldType: "json", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "标签", Display: true, Index: 10, Ctime: now, Utime: now},
		{ID: 11, FieldUID: "agent_status", FieldName: "Agent安装状态", FieldType: "enum", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "Agent安装状态", Display: true, Index: 11, Option: toJSON([]string{"已安装", "未安装"}), Ctime: now, Utime: now},
		{ID: 12, FieldUID: "associate_key", FieldName: "关联密钥", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "关联密钥", Display: true, Index: 12, Ctime: now, Utime: now},
		{ID: 13, FieldUID: "platform", FieldName: "平台", FieldType: "enum", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "平台", Display: true, Index: 13, Option: toJSON([]string{"aliyun", "aws", "huawei", "tencent", "azure"}), Ctime: now, Utime: now},
		{ID: 14, FieldUID: "charge_type", FieldName: "计费方式", FieldType: "enum", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "计费方式", Display: true, Index: 14, Option: toJSON([]string{"按量付费", "包年包月"}), Ctime: now, Utime: now},
		{ID: 15, FieldUID: "password", FieldName: "密码", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "密码", Display: false, Index: 15, Secure: true, Ctime: now, Utime: now},
		{ID: 16, FieldUID: "region", FieldName: "区域", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "区域", Display: true, Index: 16, Required: true, Ctime: now, Utime: now},
		{ID: 17, FieldUID: "zone", FieldName: "可用区", FieldType: "string", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "可用区", Display: true, Index: 17, Ctime: now, Utime: now},
		{ID: 18, FieldUID: "cloud_account_id", FieldName: "云账号", FieldType: "link", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "云账号", Display: true, Index: 18, Link: true, Ctime: now, Utime: now},
		{ID: 19, FieldUID: "cloud_subscription_id", FieldName: "云订阅", FieldType: "link", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "云订阅", Display: true, Index: 19, Link: true, Ctime: now, Utime: now},
		{ID: 20, FieldUID: "creation_time", FieldName: "创建时间", FieldType: "datetime", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "创建时间", Display: true, Index: 20, Ctime: now, Utime: now},
		{ID: 21, FieldUID: "update_time", FieldName: "更新时间", FieldType: "datetime", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "更新时间", Display: true, Index: 21, Ctime: now, Utime: now},
		{ID: 22, FieldUID: "remark", FieldName: "备注", FieldType: "text", ModelUID: "cloud_vm", GroupID: 1, DisplayName: "备注", Display: true, Index: 22, Ctime: now, Utime: now},

		// 配置信息
		{ID: 30, FieldUID: "os_type", FieldName: "操作系统", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "操作系统", Display: true, Index: 1, Ctime: now, Utime: now},
		{ID: 31, FieldUID: "private_ip", FieldName: "IP", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "IP", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 32, FieldUID: "public_ip", FieldName: "辅助IP", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "辅助IP", Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 33, FieldUID: "mac_address", FieldName: "MAC", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "MAC", Display: true, Index: 4, Ctime: now, Utime: now},
		{ID: 34, FieldUID: "os_image", FieldName: "系统镜像", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "系统镜像", Display: true, Index: 5, Ctime: now, Utime: now},
		{ID: 35, FieldUID: "hypervisor", FieldName: "宿主机", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "宿主机", Display: true, Index: 6, Ctime: now, Utime: now},
		{ID: 36, FieldUID: "security_group", FieldName: "安全组", FieldType: "link", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "安全组", Display: true, Index: 7, Link: true, Ctime: now, Utime: now},
		{ID: 37, FieldUID: "vpc_id", FieldName: "VPC", FieldType: "link", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "VPC", Display: true, Index: 8, Link: true, Ctime: now, Utime: now},
		{ID: 38, FieldUID: "cpu", FieldName: "CPU", FieldType: "int", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "CPU", Display: true, Index: 9, Ctime: now, Utime: now},
		{ID: 39, FieldUID: "memory", FieldName: "内存", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "内存", Display: true, Index: 10, Ctime: now, Utime: now},
		{ID: 40, FieldUID: "system_disk", FieldName: "系统盘", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "系统盘", Display: true, Index: 11, Ctime: now, Utime: now},
		{ID: 41, FieldUID: "data_disk", FieldName: "数据盘", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "数据盘", Display: true, Index: 12, Ctime: now, Utime: now},
		{ID: 42, FieldUID: "iso", FieldName: "ISO", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "ISO", Display: true, Index: 13, Ctime: now, Utime: now},
		{ID: 43, FieldUID: "gpu_device", FieldName: "透传设备", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "透传设备", Display: true, Index: 14, Ctime: now, Utime: now},
		{ID: 44, FieldUID: "usb", FieldName: "USB", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "USB", Display: true, Index: 15, Ctime: now, Utime: now},
		{ID: 45, FieldUID: "auto_start", FieldName: "自动启动", FieldType: "bool", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "自动启动", Display: true, Index: 16, Ctime: now, Utime: now},
		{ID: 46, FieldUID: "max_bandwidth", FieldName: "最大带宽", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "最大带宽", Display: true, Index: 17, Ctime: now, Utime: now},
		{ID: 47, FieldUID: "monitor_url", FieldName: "监控地址", FieldType: "string", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "监控地址", Display: true, Index: 18, Ctime: now, Utime: now},
		{ID: 48, FieldUID: "boot_mode", FieldName: "引导模式", FieldType: "enum", ModelUID: "cloud_vm", GroupID: 2, DisplayName: "引导模式", Display: true, Index: 19, Option: toJSON([]string{"BIOS", "UEFI"}), Ctime: now, Utime: now},

		// 加密
		{ID: 50, FieldUID: "encryption_key", FieldName: "加密密钥", FieldType: "string", ModelUID: "cloud_vm", GroupID: 5, DisplayName: "加密密钥", Display: true, Index: 1, Secure: true, Ctime: now, Utime: now},
	}

	// 阿里云ECS属性分组
	ecsAttrGroups := []AttributeGroup{
		{ID: 1, UID: "basic", Name: "基本信息", ModelUID: "aliyun_ecs", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 2, UID: "config", Name: "配置信息", ModelUID: "aliyun_ecs", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 3, UID: "network", Name: "网络信息", ModelUID: "aliyun_ecs", Index: 3, IsBuiltin: true, Ctime: now, Utime: now},
	}
	// 阿里云RDS属性分组
	rdsAttrGroups := []AttributeGroup{
		{ID: 11, UID: "basic", Name: "基本信息", ModelUID: "aliyun_rds", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 12, UID: "config", Name: "配置信息", ModelUID: "aliyun_rds", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},
	}
	// 阿里云RAM用户属性分组
	ramUserAttrGroups := []AttributeGroup{
		{ID: 21, UID: "basic", Name: "基本信息", ModelUID: "aliyun_ram_user", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 22, UID: "contact", Name: "联系方式", ModelUID: "aliyun_ram_user", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},
	}

	// 阿里云ECS字段
	ecsFields := []ModelField{
		{ID: 1, FieldUID: "instance_id", FieldName: "实例ID", FieldType: "string", ModelUID: "aliyun_ecs", GroupID: 1, Display: true, Index: 1, Required: true, Ctime: now, Utime: now},
		{ID: 2, FieldUID: "instance_name", FieldName: "实例名称", FieldType: "string", ModelUID: "aliyun_ecs", GroupID: 1, Display: true, Index: 2, Required: true, Ctime: now, Utime: now},
		{ID: 3, FieldUID: "status", FieldName: "状态", FieldType: "enum", ModelUID: "aliyun_ecs", GroupID: 1, Display: true, Index: 3, Required: true, Option: toJSON([]string{"Running", "Stopped", "Starting", "Stopping"}), Ctime: now, Utime: now},
		{ID: 4, FieldUID: "region", FieldName: "地域", FieldType: "string", ModelUID: "aliyun_ecs", GroupID: 1, Display: true, Index: 4, Required: true, Ctime: now, Utime: now},
		{ID: 5, FieldUID: "zone", FieldName: "可用区", FieldType: "string", ModelUID: "aliyun_ecs", GroupID: 1, Display: true, Index: 5, Ctime: now, Utime: now},
		{ID: 6, FieldUID: "cpu", FieldName: "CPU核数", FieldType: "int", ModelUID: "aliyun_ecs", GroupID: 2, Display: true, Index: 1, Ctime: now, Utime: now},
		{ID: 7, FieldUID: "memory", FieldName: "内存(MB)", FieldType: "int", ModelUID: "aliyun_ecs", GroupID: 2, Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 8, FieldUID: "os_type", FieldName: "操作系统", FieldType: "string", ModelUID: "aliyun_ecs", GroupID: 2, Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 9, FieldUID: "instance_type", FieldName: "实例规格", FieldType: "string", ModelUID: "aliyun_ecs", GroupID: 2, Display: true, Index: 4, Ctime: now, Utime: now},
		{ID: 10, FieldUID: "private_ip", FieldName: "私网IP", FieldType: "string", ModelUID: "aliyun_ecs", GroupID: 3, Display: true, Index: 1, Ctime: now, Utime: now},
		{ID: 11, FieldUID: "public_ip", FieldName: "公网IP", FieldType: "string", ModelUID: "aliyun_ecs", GroupID: 3, Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 12, FieldUID: "vpc_id", FieldName: "VPC ID", FieldType: "link", ModelUID: "aliyun_ecs", GroupID: 3, Display: true, Index: 3, Link: true, LinkModel: "aliyun_vpc", Ctime: now, Utime: now},
		{ID: 13, FieldUID: "creation_time", FieldName: "创建时间", FieldType: "datetime", ModelUID: "aliyun_ecs", GroupID: 1, Display: true, Index: 6, Ctime: now, Utime: now},
	}

	// 阿里云RDS字段
	rdsFields := []ModelField{
		{ID: 101, FieldUID: "db_instance_id", FieldName: "实例ID", FieldType: "string", ModelUID: "aliyun_rds", GroupID: 11, Display: true, Index: 1, Required: true, Ctime: now, Utime: now},
		{ID: 102, FieldUID: "db_instance_description", FieldName: "实例描述", FieldType: "string", ModelUID: "aliyun_rds", GroupID: 11, Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 103, FieldUID: "status", FieldName: "状态", FieldType: "enum", ModelUID: "aliyun_rds", GroupID: 11, Display: true, Index: 3, Required: true, Ctime: now, Utime: now},
		{ID: 104, FieldUID: "region", FieldName: "地域", FieldType: "string", ModelUID: "aliyun_rds", GroupID: 11, Display: true, Index: 4, Required: true, Ctime: now, Utime: now},
		{ID: 105, FieldUID: "engine", FieldName: "数据库引擎", FieldType: "enum", ModelUID: "aliyun_rds", GroupID: 12, Display: true, Index: 1, Option: toJSON([]string{"MySQL", "PostgreSQL", "SQLServer", "MariaDB"}), Ctime: now, Utime: now},
		{ID: 106, FieldUID: "engine_version", FieldName: "引擎版本", FieldType: "string", ModelUID: "aliyun_rds", GroupID: 12, Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 107, FieldUID: "db_instance_class", FieldName: "实例规格", FieldType: "string", ModelUID: "aliyun_rds", GroupID: 12, Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 108, FieldUID: "db_instance_storage", FieldName: "存储空间(GB)", FieldType: "int", ModelUID: "aliyun_rds", GroupID: 12, Display: true, Index: 4, Ctime: now, Utime: now},
		{ID: 109, FieldUID: "connection_string", FieldName: "连接地址", FieldType: "string", ModelUID: "aliyun_rds", GroupID: 12, Display: true, Index: 5, Ctime: now, Utime: now},
		{ID: 110, FieldUID: "port", FieldName: "端口", FieldType: "int", ModelUID: "aliyun_rds", GroupID: 12, Display: true, Index: 6, Ctime: now, Utime: now},
	}

	// 阿里云RAM用户字段
	ramUserFields := []ModelField{
		{ID: 201, FieldUID: "user_id", FieldName: "用户ID", FieldType: "string", ModelUID: "aliyun_ram_user", GroupID: 21, Display: true, Index: 1, Required: true, Ctime: now, Utime: now},
		{ID: 202, FieldUID: "user_name", FieldName: "用户名", FieldType: "string", ModelUID: "aliyun_ram_user", GroupID: 21, Display: true, Index: 2, Required: true, Ctime: now, Utime: now},
		{ID: 203, FieldUID: "display_name", FieldName: "显示名称", FieldType: "string", ModelUID: "aliyun_ram_user", GroupID: 21, Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 204, FieldUID: "email", FieldName: "邮箱", FieldType: "string", ModelUID: "aliyun_ram_user", GroupID: 22, Display: true, Index: 1, Ctime: now, Utime: now},
		{ID: 205, FieldUID: "mobile_phone", FieldName: "手机号", FieldType: "string", ModelUID: "aliyun_ram_user", GroupID: 22, Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 206, FieldUID: "create_date", FieldName: "创建时间", FieldType: "datetime", ModelUID: "aliyun_ram_user", GroupID: 21, Display: true, Index: 4, Ctime: now, Utime: now},
		{ID: 207, FieldUID: "last_login_date", FieldName: "最后登录时间", FieldType: "datetime", ModelUID: "aliyun_ram_user", GroupID: 21, Display: true, Index: 5, Ctime: now, Utime: now},
	}

	// ==================== 通用云资源模型属性 ====================

	// 通用 RDS 属性分组
	cloudRdsAttrGroups := []AttributeGroup{
		{ID: 100, UID: "basic", Name: "基本信息", ModelUID: "cloud_rds", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 101, UID: "config", Name: "配置信息", ModelUID: "cloud_rds", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 102, UID: "network", Name: "网络信息", ModelUID: "cloud_rds", Index: 3, IsBuiltin: true, Ctime: now, Utime: now},
	}
	cloudRdsFields := []ModelField{
		{ID: 1001, FieldUID: "instance_id", FieldName: "实例ID", FieldType: "string", ModelUID: "cloud_rds", GroupID: 100, DisplayName: "实例ID", Display: true, Index: 1, Required: true, Ctime: now, Utime: now},
		{ID: 1002, FieldUID: "instance_name", FieldName: "实例名称", FieldType: "string", ModelUID: "cloud_rds", GroupID: 100, DisplayName: "实例名称", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 1003, FieldUID: "status", FieldName: "状态", FieldType: "string", ModelUID: "cloud_rds", GroupID: 100, DisplayName: "状态", Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 1004, FieldUID: "region", FieldName: "地域", FieldType: "string", ModelUID: "cloud_rds", GroupID: 100, DisplayName: "地域", Display: true, Index: 4, Ctime: now, Utime: now},
		{ID: 1005, FieldUID: "zone", FieldName: "可用区", FieldType: "string", ModelUID: "cloud_rds", GroupID: 100, DisplayName: "可用区", Display: true, Index: 5, Ctime: now, Utime: now},
		{ID: 1006, FieldUID: "provider", FieldName: "云厂商", FieldType: "string", ModelUID: "cloud_rds", GroupID: 100, DisplayName: "云厂商", Display: true, Index: 6, Ctime: now, Utime: now},
		{ID: 1007, FieldUID: "engine", FieldName: "数据库引擎", FieldType: "string", ModelUID: "cloud_rds", GroupID: 101, DisplayName: "数据库引擎", Display: true, Index: 1, Ctime: now, Utime: now},
		{ID: 1008, FieldUID: "engine_version", FieldName: "引擎版本", FieldType: "string", ModelUID: "cloud_rds", GroupID: 101, DisplayName: "引擎版本", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 1009, FieldUID: "instance_class", FieldName: "实例规格", FieldType: "string", ModelUID: "cloud_rds", GroupID: 101, DisplayName: "实例规格", Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 1010, FieldUID: "cpu", FieldName: "CPU", FieldType: "int", ModelUID: "cloud_rds", GroupID: 101, DisplayName: "CPU", Display: true, Index: 4, Ctime: now, Utime: now},
		{ID: 1011, FieldUID: "memory", FieldName: "内存(MB)", FieldType: "int", ModelUID: "cloud_rds", GroupID: 101, DisplayName: "内存(MB)", Display: true, Index: 5, Ctime: now, Utime: now},
		{ID: 1012, FieldUID: "storage", FieldName: "存储(GB)", FieldType: "int", ModelUID: "cloud_rds", GroupID: 101, DisplayName: "存储(GB)", Display: true, Index: 6, Ctime: now, Utime: now},
		{ID: 1013, FieldUID: "connection_string", FieldName: "连接地址", FieldType: "string", ModelUID: "cloud_rds", GroupID: 102, DisplayName: "连接地址", Display: true, Index: 1, Ctime: now, Utime: now},
		{ID: 1014, FieldUID: "port", FieldName: "端口", FieldType: "int", ModelUID: "cloud_rds", GroupID: 102, DisplayName: "端口", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 1015, FieldUID: "vpc_id", FieldName: "VPC ID", FieldType: "string", ModelUID: "cloud_rds", GroupID: 102, DisplayName: "VPC ID", Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 1016, FieldUID: "private_ip", FieldName: "私网IP", FieldType: "string", ModelUID: "cloud_rds", GroupID: 102, DisplayName: "私网IP", Display: true, Index: 4, Ctime: now, Utime: now},
	}

	// 通用 Redis 属性分组
	cloudRedisAttrGroups := []AttributeGroup{
		{ID: 110, UID: "basic", Name: "基本信息", ModelUID: "cloud_redis", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 111, UID: "config", Name: "配置信息", ModelUID: "cloud_redis", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 112, UID: "network", Name: "网络信息", ModelUID: "cloud_redis", Index: 3, IsBuiltin: true, Ctime: now, Utime: now},
	}
	cloudRedisFields := []ModelField{
		{ID: 1101, FieldUID: "instance_id", FieldName: "实例ID", FieldType: "string", ModelUID: "cloud_redis", GroupID: 110, DisplayName: "实例ID", Display: true, Index: 1, Required: true, Ctime: now, Utime: now},
		{ID: 1102, FieldUID: "instance_name", FieldName: "实例名称", FieldType: "string", ModelUID: "cloud_redis", GroupID: 110, DisplayName: "实例名称", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 1103, FieldUID: "status", FieldName: "状态", FieldType: "string", ModelUID: "cloud_redis", GroupID: 110, DisplayName: "状态", Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 1104, FieldUID: "region", FieldName: "地域", FieldType: "string", ModelUID: "cloud_redis", GroupID: 110, DisplayName: "地域", Display: true, Index: 4, Ctime: now, Utime: now},
		{ID: 1105, FieldUID: "provider", FieldName: "云厂商", FieldType: "string", ModelUID: "cloud_redis", GroupID: 110, DisplayName: "云厂商", Display: true, Index: 5, Ctime: now, Utime: now},
		{ID: 1106, FieldUID: "engine_version", FieldName: "引擎版本", FieldType: "string", ModelUID: "cloud_redis", GroupID: 111, DisplayName: "引擎版本", Display: true, Index: 1, Ctime: now, Utime: now},
		{ID: 1107, FieldUID: "instance_class", FieldName: "实例规格", FieldType: "string", ModelUID: "cloud_redis", GroupID: 111, DisplayName: "实例规格", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 1108, FieldUID: "architecture", FieldName: "架构类型", FieldType: "string", ModelUID: "cloud_redis", GroupID: 111, DisplayName: "架构类型", Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 1109, FieldUID: "capacity", FieldName: "容量(MB)", FieldType: "int", ModelUID: "cloud_redis", GroupID: 111, DisplayName: "容量(MB)", Display: true, Index: 4, Ctime: now, Utime: now},
		{ID: 1110, FieldUID: "bandwidth", FieldName: "带宽(Mbps)", FieldType: "int", ModelUID: "cloud_redis", GroupID: 111, DisplayName: "带宽(Mbps)", Display: true, Index: 5, Ctime: now, Utime: now},
		{ID: 1111, FieldUID: "connection_domain", FieldName: "连接地址", FieldType: "string", ModelUID: "cloud_redis", GroupID: 112, DisplayName: "连接地址", Display: true, Index: 1, Ctime: now, Utime: now},
		{ID: 1112, FieldUID: "port", FieldName: "端口", FieldType: "int", ModelUID: "cloud_redis", GroupID: 112, DisplayName: "端口", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 1113, FieldUID: "vpc_id", FieldName: "VPC ID", FieldType: "string", ModelUID: "cloud_redis", GroupID: 112, DisplayName: "VPC ID", Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 1114, FieldUID: "private_ip", FieldName: "私网IP", FieldType: "string", ModelUID: "cloud_redis", GroupID: 112, DisplayName: "私网IP", Display: true, Index: 4, Ctime: now, Utime: now},
	}

	// 通用 MongoDB 属性分组
	cloudMongodbAttrGroups := []AttributeGroup{
		{ID: 120, UID: "basic", Name: "基本信息", ModelUID: "cloud_mongodb", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 121, UID: "config", Name: "配置信息", ModelUID: "cloud_mongodb", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 122, UID: "network", Name: "网络信息", ModelUID: "cloud_mongodb", Index: 3, IsBuiltin: true, Ctime: now, Utime: now},
	}
	cloudMongodbFields := []ModelField{
		{ID: 1201, FieldUID: "instance_id", FieldName: "实例ID", FieldType: "string", ModelUID: "cloud_mongodb", GroupID: 120, DisplayName: "实例ID", Display: true, Index: 1, Required: true, Ctime: now, Utime: now},
		{ID: 1202, FieldUID: "instance_name", FieldName: "实例名称", FieldType: "string", ModelUID: "cloud_mongodb", GroupID: 120, DisplayName: "实例名称", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 1203, FieldUID: "status", FieldName: "状态", FieldType: "string", ModelUID: "cloud_mongodb", GroupID: 120, DisplayName: "状态", Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 1204, FieldUID: "region", FieldName: "地域", FieldType: "string", ModelUID: "cloud_mongodb", GroupID: 120, DisplayName: "地域", Display: true, Index: 4, Ctime: now, Utime: now},
		{ID: 1205, FieldUID: "provider", FieldName: "云厂商", FieldType: "string", ModelUID: "cloud_mongodb", GroupID: 120, DisplayName: "云厂商", Display: true, Index: 5, Ctime: now, Utime: now},
		{ID: 1206, FieldUID: "engine_version", FieldName: "引擎版本", FieldType: "string", ModelUID: "cloud_mongodb", GroupID: 121, DisplayName: "引擎版本", Display: true, Index: 1, Ctime: now, Utime: now},
		{ID: 1207, FieldUID: "instance_class", FieldName: "实例规格", FieldType: "string", ModelUID: "cloud_mongodb", GroupID: 121, DisplayName: "实例规格", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 1208, FieldUID: "db_type", FieldName: "数据库类型", FieldType: "string", ModelUID: "cloud_mongodb", GroupID: 121, DisplayName: "数据库类型", Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 1209, FieldUID: "cpu", FieldName: "CPU", FieldType: "int", ModelUID: "cloud_mongodb", GroupID: 121, DisplayName: "CPU", Display: true, Index: 4, Ctime: now, Utime: now},
		{ID: 1210, FieldUID: "memory", FieldName: "内存(MB)", FieldType: "int", ModelUID: "cloud_mongodb", GroupID: 121, DisplayName: "内存(MB)", Display: true, Index: 5, Ctime: now, Utime: now},
		{ID: 1211, FieldUID: "storage", FieldName: "存储(GB)", FieldType: "int", ModelUID: "cloud_mongodb", GroupID: 121, DisplayName: "存储(GB)", Display: true, Index: 6, Ctime: now, Utime: now},
		{ID: 1212, FieldUID: "connection_string", FieldName: "连接地址", FieldType: "string", ModelUID: "cloud_mongodb", GroupID: 122, DisplayName: "连接地址", Display: true, Index: 1, Ctime: now, Utime: now},
		{ID: 1213, FieldUID: "port", FieldName: "端口", FieldType: "int", ModelUID: "cloud_mongodb", GroupID: 122, DisplayName: "端口", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 1214, FieldUID: "vpc_id", FieldName: "VPC ID", FieldType: "string", ModelUID: "cloud_mongodb", GroupID: 122, DisplayName: "VPC ID", Display: true, Index: 3, Ctime: now, Utime: now},
	}

	// 通用 VPC 属性分组
	cloudVpcAttrGroups := []AttributeGroup{
		{ID: 130, UID: "basic", Name: "基本信息", ModelUID: "cloud_vpc", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 131, UID: "network", Name: "网络配置", ModelUID: "cloud_vpc", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 132, UID: "resource", Name: "关联资源", ModelUID: "cloud_vpc", Index: 3, IsBuiltin: true, Ctime: now, Utime: now},
	}
	cloudVpcFields := []ModelField{
		{ID: 1301, FieldUID: "vpc_id", FieldName: "VPC ID", FieldType: "string", ModelUID: "cloud_vpc", GroupID: 130, DisplayName: "VPC ID", Display: true, Index: 1, Required: true, Ctime: now, Utime: now},
		{ID: 1302, FieldUID: "vpc_name", FieldName: "VPC名称", FieldType: "string", ModelUID: "cloud_vpc", GroupID: 130, DisplayName: "VPC名称", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 1303, FieldUID: "status", FieldName: "状态", FieldType: "string", ModelUID: "cloud_vpc", GroupID: 130, DisplayName: "状态", Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 1304, FieldUID: "region", FieldName: "地域", FieldType: "string", ModelUID: "cloud_vpc", GroupID: 130, DisplayName: "地域", Display: true, Index: 4, Ctime: now, Utime: now},
		{ID: 1305, FieldUID: "provider", FieldName: "云厂商", FieldType: "string", ModelUID: "cloud_vpc", GroupID: 130, DisplayName: "云厂商", Display: true, Index: 5, Ctime: now, Utime: now},
		{ID: 1306, FieldUID: "is_default", FieldName: "默认VPC", FieldType: "bool", ModelUID: "cloud_vpc", GroupID: 130, DisplayName: "默认VPC", Display: true, Index: 6, Ctime: now, Utime: now},
		{ID: 1307, FieldUID: "cidr_block", FieldName: "CIDR", FieldType: "string", ModelUID: "cloud_vpc", GroupID: 131, DisplayName: "CIDR", Display: true, Index: 1, Ctime: now, Utime: now},
		{ID: 1308, FieldUID: "ipv6_cidr_block", FieldName: "IPv6 CIDR", FieldType: "string", ModelUID: "cloud_vpc", GroupID: 131, DisplayName: "IPv6 CIDR", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 1309, FieldUID: "enable_ipv6", FieldName: "启用IPv6", FieldType: "bool", ModelUID: "cloud_vpc", GroupID: 131, DisplayName: "启用IPv6", Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 1310, FieldUID: "vswitch_count", FieldName: "交换机数量", FieldType: "int", ModelUID: "cloud_vpc", GroupID: 132, DisplayName: "交换机数量", Display: true, Index: 1, Ctime: now, Utime: now},
		{ID: 1311, FieldUID: "route_table_count", FieldName: "路由表数量", FieldType: "int", ModelUID: "cloud_vpc", GroupID: 132, DisplayName: "路由表数量", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 1312, FieldUID: "nat_gateway_count", FieldName: "NAT网关数量", FieldType: "int", ModelUID: "cloud_vpc", GroupID: 132, DisplayName: "NAT网关数量", Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 1313, FieldUID: "security_group_count", FieldName: "安全组数量", FieldType: "int", ModelUID: "cloud_vpc", GroupID: 132, DisplayName: "安全组数量", Display: true, Index: 4, Ctime: now, Utime: now},
	}

	// 通用 EIP 属性分组
	cloudEipAttrGroups := []AttributeGroup{
		{ID: 140, UID: "basic", Name: "基本信息", ModelUID: "cloud_eip", Index: 1, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 141, UID: "bindinfo", Name: "绑定信息", ModelUID: "cloud_eip", Index: 2, IsBuiltin: true, Ctime: now, Utime: now},
		{ID: 142, UID: "billing", Name: "计费信息", ModelUID: "cloud_eip", Index: 3, IsBuiltin: true, Ctime: now, Utime: now},
	}
	cloudEipFields := []ModelField{
		{ID: 1401, FieldUID: "allocation_id", FieldName: "EIP ID", FieldType: "string", ModelUID: "cloud_eip", GroupID: 140, DisplayName: "EIP ID", Display: true, Index: 1, Required: true, Ctime: now, Utime: now},
		{ID: 1402, FieldUID: "ip_address", FieldName: "IP地址", FieldType: "string", ModelUID: "cloud_eip", GroupID: 140, DisplayName: "IP地址", Display: true, Index: 2, Required: true, Ctime: now, Utime: now},
		{ID: 1403, FieldUID: "name", FieldName: "名称", FieldType: "string", ModelUID: "cloud_eip", GroupID: 140, DisplayName: "名称", Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 1404, FieldUID: "status", FieldName: "状态", FieldType: "string", ModelUID: "cloud_eip", GroupID: 140, DisplayName: "状态", Display: true, Index: 4, Ctime: now, Utime: now},
		{ID: 1405, FieldUID: "region", FieldName: "地域", FieldType: "string", ModelUID: "cloud_eip", GroupID: 140, DisplayName: "地域", Display: true, Index: 5, Ctime: now, Utime: now},
		{ID: 1406, FieldUID: "provider", FieldName: "云厂商", FieldType: "string", ModelUID: "cloud_eip", GroupID: 140, DisplayName: "云厂商", Display: true, Index: 6, Ctime: now, Utime: now},
		{ID: 1407, FieldUID: "isp", FieldName: "线路类型", FieldType: "string", ModelUID: "cloud_eip", GroupID: 140, DisplayName: "线路类型", Display: true, Index: 7, Ctime: now, Utime: now},
		{ID: 1408, FieldUID: "instance_id", FieldName: "绑定实例ID", FieldType: "string", ModelUID: "cloud_eip", GroupID: 141, DisplayName: "绑定实例ID", Display: true, Index: 1, Ctime: now, Utime: now},
		{ID: 1409, FieldUID: "instance_type", FieldName: "绑定实例类型", FieldType: "string", ModelUID: "cloud_eip", GroupID: 141, DisplayName: "绑定实例类型", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 1410, FieldUID: "instance_name", FieldName: "绑定实例名称", FieldType: "string", ModelUID: "cloud_eip", GroupID: 141, DisplayName: "绑定实例名称", Display: true, Index: 3, Ctime: now, Utime: now},
		{ID: 1411, FieldUID: "vpc_id", FieldName: "VPC ID", FieldType: "string", ModelUID: "cloud_eip", GroupID: 141, DisplayName: "VPC ID", Display: true, Index: 4, Ctime: now, Utime: now},
		{ID: 1412, FieldUID: "bandwidth", FieldName: "带宽(Mbps)", FieldType: "int", ModelUID: "cloud_eip", GroupID: 142, DisplayName: "带宽(Mbps)", Display: true, Index: 1, Ctime: now, Utime: now},
		{ID: 1413, FieldUID: "internet_charge_type", FieldName: "计费方式", FieldType: "string", ModelUID: "cloud_eip", GroupID: 142, DisplayName: "计费方式", Display: true, Index: 2, Ctime: now, Utime: now},
		{ID: 1414, FieldUID: "charge_type", FieldName: "付费类型", FieldType: "string", ModelUID: "cloud_eip", GroupID: 142, DisplayName: "付费类型", Display: true, Index: 3, Ctime: now, Utime: now},
	}

	// ==================== 关系类型定义 ====================
	relationTypes := []RelationType{
		// ECS 关系
		{ID: 1, UID: "ecs_belongs_to_vpc", Name: "ECS属于VPC", SourceModelUID: "cloud_vm", TargetModelUID: "cloud_vpc", Direction: "many_to_one", Description: "ECS实例所属的VPC", Ctime: now, Utime: now},
		// EIP 关系
		{ID: 2, UID: "eip_bindto_ecs", Name: "EIP绑定ECS", SourceModelUID: "cloud_eip", TargetModelUID: "cloud_vm", Direction: "one_to_one", Description: "EIP绑定到ECS实例", Ctime: now, Utime: now},
		{ID: 3, UID: "eip_belongs_to_vpc", Name: "EIP属于VPC", SourceModelUID: "cloud_eip", TargetModelUID: "cloud_vpc", Direction: "many_to_one", Description: "EIP所属的VPC", Ctime: now, Utime: now},
		// RDS 关系
		{ID: 4, UID: "rds_belongs_to_vpc", Name: "RDS属于VPC", SourceModelUID: "cloud_rds", TargetModelUID: "cloud_vpc", Direction: "many_to_one", Description: "RDS实例所属的VPC", Ctime: now, Utime: now},
		// Redis 关系
		{ID: 5, UID: "redis_belongs_to_vpc", Name: "Redis属于VPC", SourceModelUID: "cloud_redis", TargetModelUID: "cloud_vpc", Direction: "many_to_one", Description: "Redis实例所属的VPC", Ctime: now, Utime: now},
		// MongoDB 关系
		{ID: 6, UID: "mongodb_belongs_to_vpc", Name: "MongoDB属于VPC", SourceModelUID: "cloud_mongodb", TargetModelUID: "cloud_vpc", Direction: "many_to_one", Description: "MongoDB实例所属的VPC", Ctime: now, Utime: now},
	}

	// 插入数据
	fmt.Println("Inserting model groups...")
	for _, g := range groups {
		_, err := db.Collection("c_model_group").UpdateOne(ctx,
			bson.M{"id": g.ID},
			bson.M{"$set": g},
			options.Update().SetUpsert(true))
		if err != nil {
			log.Printf("Failed to insert group %s: %v", g.Name, err)
		}
	}

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

	fmt.Println("Inserting attribute groups...")
	allAttrGroups := append(vmAttrGroups, ecsAttrGroups...)
	allAttrGroups = append(allAttrGroups, rdsAttrGroups...)
	allAttrGroups = append(allAttrGroups, ramUserAttrGroups...)
	// 添加通用云资源模型属性分组
	allAttrGroups = append(allAttrGroups, cloudRdsAttrGroups...)
	allAttrGroups = append(allAttrGroups, cloudRedisAttrGroups...)
	allAttrGroups = append(allAttrGroups, cloudMongodbAttrGroups...)
	allAttrGroups = append(allAttrGroups, cloudVpcAttrGroups...)
	allAttrGroups = append(allAttrGroups, cloudEipAttrGroups...)
	for _, g := range allAttrGroups {
		_, err := db.Collection("c_attribute_group").UpdateOne(ctx,
			bson.M{"model_uid": g.ModelUID, "uid": g.UID},
			bson.M{"$set": g},
			options.Update().SetUpsert(true))
		if err != nil {
			log.Printf("Failed to insert attribute group %s.%s: %v", g.ModelUID, g.UID, err)
		}
	}

	fmt.Println("Inserting fields...")
	allFields := append(vmFields, ecsFields...)
	allFields = append(allFields, rdsFields...)
	allFields = append(allFields, ramUserFields...)
	// 添加通用云资源模型字段
	allFields = append(allFields, cloudRdsFields...)
	allFields = append(allFields, cloudRedisFields...)
	allFields = append(allFields, cloudMongodbFields...)
	allFields = append(allFields, cloudVpcFields...)
	allFields = append(allFields, cloudEipFields...)
	for _, f := range allFields {
		_, err := db.Collection("c_attribute").UpdateOne(ctx,
			bson.M{"model_uid": f.ModelUID, "field_uid": f.FieldUID},
			bson.M{"$set": f},
			options.Update().SetUpsert(true))
		if err != nil {
			log.Printf("Failed to insert field %s.%s: %v", f.ModelUID, f.FieldUID, err)
		}
	}

	fmt.Println("Inserting relation types...")
	for _, rt := range relationTypes {
		_, err := db.Collection("c_model_relation_type").UpdateOne(ctx,
			bson.M{"uid": rt.UID},
			bson.M{"$set": rt},
			options.Update().SetUpsert(true))
		if err != nil {
			log.Printf("Failed to insert relation type %s: %v", rt.UID, err)
		}
	}

	fmt.Println("Done!")
}

func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

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

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 连接MongoDB
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)

	db := client.Database("e_cam")
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
		// 通用云资源模型
		{ID: 1, UID: "cloud_vm", Name: "虚拟机", ModelGroupID: 1, Category: "compute", Level: 1, Provider: "all", Icon: "server", Description: "云服务器/虚拟机", Extensible: true, Ctime: now, Utime: now},

		// 阿里云
		{ID: 11, UID: "aliyun_ecs", Name: "阿里云ECS", ModelGroupID: 1, ParentUID: "cloud_vm", Category: "compute", Level: 2, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 12, UID: "aliyun_rds", Name: "阿里云RDS", ModelGroupID: 4, Category: "database", Level: 1, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 13, UID: "aliyun_oss", Name: "阿里云OSS", ModelGroupID: 2, Category: "storage", Level: 1, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 14, UID: "aliyun_vpc", Name: "阿里云VPC", ModelGroupID: 3, Category: "network", Level: 1, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 15, UID: "aliyun_slb", Name: "阿里云SLB", ModelGroupID: 3, Category: "network", Level: 1, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 16, UID: "aliyun_security_group", Name: "阿里云安全组", ModelGroupID: 5, Category: "security", Level: 1, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 17, UID: "aliyun_ram_user", Name: "阿里云RAM用户", ModelGroupID: 6, Category: "iam", Level: 1, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 18, UID: "aliyun_ram_group", Name: "阿里云RAM用户组", ModelGroupID: 6, Category: "iam", Level: 1, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},
		{ID: 19, UID: "aliyun_ram_policy", Name: "阿里云RAM策略", ModelGroupID: 6, Category: "iam", Level: 1, Provider: "aliyun", Extensible: true, Ctime: now, Utime: now},

		// AWS
		{ID: 101, UID: "aws_ec2", Name: "AWS EC2", ModelGroupID: 1, ParentUID: "cloud_vm", Category: "compute", Level: 2, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 102, UID: "aws_rds", Name: "AWS RDS", ModelGroupID: 4, Category: "database", Level: 1, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 103, UID: "aws_s3", Name: "AWS S3", ModelGroupID: 2, Category: "storage", Level: 1, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 104, UID: "aws_vpc", Name: "AWS VPC", ModelGroupID: 3, Category: "network", Level: 1, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 105, UID: "aws_iam_user", Name: "AWS IAM用户", ModelGroupID: 6, Category: "iam", Level: 1, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 106, UID: "aws_iam_group", Name: "AWS IAM用户组", ModelGroupID: 6, Category: "iam", Level: 1, Provider: "aws", Extensible: true, Ctime: now, Utime: now},
		{ID: 107, UID: "aws_iam_policy", Name: "AWS IAM策略", ModelGroupID: 6, Category: "iam", Level: 1, Provider: "aws", Extensible: true, Ctime: now, Utime: now},

		// 华为云
		{ID: 201, UID: "huawei_ecs", Name: "华为云ECS", ModelGroupID: 1, ParentUID: "cloud_vm", Category: "compute", Level: 2, Provider: "huawei", Extensible: true, Ctime: now, Utime: now},
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
	for _, f := range allFields {
		_, err := db.Collection("c_attribute").UpdateOne(ctx,
			bson.M{"model_uid": f.ModelUID, "field_uid": f.FieldUID},
			bson.M{"$set": f},
			options.Update().SetUpsert(true))
		if err != nil {
			log.Printf("Failed to insert field %s.%s: %v", f.ModelUID, f.FieldUID, err)
		}
	}

	fmt.Println("Done!")
}

func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

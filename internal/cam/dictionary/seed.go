package dictionary

import (
	"context"
	"log"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
)

// SeedType 种子字典类型定义
type SeedType struct {
	Code        string
	Name        string
	Description string
	Items       []SeedItem
}

// SeedItem 种子字典项定义
type SeedItem struct {
	Value     string
	Label     string
	SortOrder int
	Extra     map[string]interface{}
}

// seedTypes 包含所有 19 个字典类型及其字典项
var seedTypes = []SeedType{
	{
		Code:        "cloud_provider",
		Name:        "云厂商",
		Description: "支持的云服务提供商列表",
		Items: []SeedItem{
			{Value: "aliyun", Label: "阿里云", SortOrder: 1, Extra: map[string]interface{}{"icon": "icon-aliyun", "color": "#ff6a00"}},
			{Value: "aws", Label: "AWS", SortOrder: 2, Extra: map[string]interface{}{"icon": "icon-aws", "color": "#ff9900"}},
			{Value: "azure", Label: "Azure", SortOrder: 3, Extra: map[string]interface{}{"icon": "icon-azure", "color": "#0078d4"}},
			{Value: "tencent", Label: "腾讯云", SortOrder: 4, Extra: map[string]interface{}{"icon": "icon-tencent", "color": "#006eff"}},
			{Value: "huawei", Label: "华为云", SortOrder: 5, Extra: map[string]interface{}{"icon": "icon-huawei", "color": "#ff0000"}},
			{Value: "volcano", Label: "火山引擎", SortOrder: 6, Extra: map[string]interface{}{"icon": "icon-volcano", "color": "#3370ff"}},
		},
	},
	{
		Code:        "asset_status",
		Name:        "资产状态",
		Description: "云资产运行状态",
		Items: []SeedItem{
			{Value: "running", Label: "运行中", SortOrder: 1, Extra: map[string]interface{}{"color": "success"}},
			{Value: "stopped", Label: "已停止", SortOrder: 2, Extra: map[string]interface{}{"color": "info"}},
			{Value: "starting", Label: "启动中", SortOrder: 3, Extra: map[string]interface{}{"color": "warning"}},
			{Value: "stopping", Label: "停止中", SortOrder: 4, Extra: map[string]interface{}{"color": "warning"}},
			{Value: "pending", Label: "创建中", SortOrder: 5, Extra: map[string]interface{}{"color": "warning"}},
			{Value: "rebooting", Label: "重启中", SortOrder: 6, Extra: map[string]interface{}{"color": "warning"}},
			{Value: "terminated", Label: "已销毁", SortOrder: 7, Extra: map[string]interface{}{"color": "danger"}},
			{Value: "deleted", Label: "已删除", SortOrder: 8, Extra: map[string]interface{}{"color": "danger"}},
			{Value: "error", Label: "异常", SortOrder: 9, Extra: map[string]interface{}{"color": "danger"}},
			{Value: "unknown", Label: "未知", SortOrder: 10, Extra: map[string]interface{}{"color": "info"}},
		},
	},
	{
		Code:        "asset_type",
		Name:        "资产类型",
		Description: "云资产分类",
		Items: []SeedItem{
			{Value: "compute", Label: "计算服务", SortOrder: 1},
			{Value: "database", Label: "数据库服务", SortOrder: 2},
			{Value: "storage", Label: "存储服务", SortOrder: 3},
			{Value: "network", Label: "网络服务", SortOrder: 4},
			{Value: "middleware", Label: "中间件服务", SortOrder: 5},
			{Value: "security", Label: "安全服务", SortOrder: 6},
			{Value: "monitor", Label: "监控服务", SortOrder: 7},
		},
	},
	{
		Code:        "service_type",
		Name:        "服务分类",
		Description: "云服务分类",
		Items: []SeedItem{
			{Value: "compute", Label: "计算", SortOrder: 1},
			{Value: "storage", Label: "存储", SortOrder: 2},
			{Value: "network", Label: "网络", SortOrder: 3},
			{Value: "database", Label: "数据库", SortOrder: 4},
			{Value: "middleware", Label: "中间件", SortOrder: 5},
			{Value: "other", Label: "其他", SortOrder: 6},
		},
	},
	{
		Code:        "account_status",
		Name:        "账号状态",
		Description: "云账号状态",
		Items: []SeedItem{
			{Value: "active", Label: "活跃", SortOrder: 1, Extra: map[string]interface{}{"color": "success"}},
			{Value: "disabled", Label: "禁用", SortOrder: 2, Extra: map[string]interface{}{"color": "info"}},
			{Value: "error", Label: "错误", SortOrder: 3, Extra: map[string]interface{}{"color": "danger"}},
			{Value: "testing", Label: "测试中", SortOrder: 4, Extra: map[string]interface{}{"color": "warning"}},
			{Value: "inactive", Label: "未激活", SortOrder: 5, Extra: map[string]interface{}{"color": "info"}},
		},
	},
	{
		Code:        "environment",
		Name:        "环境类型",
		Description: "部署环境类型",
		Items: []SeedItem{
			{Value: "production", Label: "生产环境", SortOrder: 1, Extra: map[string]interface{}{"color": "danger"}},
			{Value: "staging", Label: "预发环境", SortOrder: 2, Extra: map[string]interface{}{"color": "warning"}},
			{Value: "development", Label: "开发环境", SortOrder: 3, Extra: map[string]interface{}{"color": "success"}},
			{Value: "testing", Label: "测试环境", SortOrder: 4, Extra: map[string]interface{}{"color": "info"}},
			{Value: "dr", Label: "灾备环境", SortOrder: 5, Extra: map[string]interface{}{"color": "warning"}},
			{Value: "sandbox", Label: "沙箱环境", SortOrder: 6, Extra: map[string]interface{}{"color": "info"}},
		},
	},
	{
		Code:        "tenant_status",
		Name:        "租户状态",
		Description: "租户状态",
		Items: []SeedItem{
			{Value: "active", Label: "活跃", SortOrder: 1, Extra: map[string]interface{}{"color": "success"}},
			{Value: "inactive", Label: "非活跃", SortOrder: 2, Extra: map[string]interface{}{"color": "info"}},
			{Value: "suspended", Label: "暂停", SortOrder: 3, Extra: map[string]interface{}{"color": "warning"}},
			{Value: "deleted", Label: "已删除", SortOrder: 4, Extra: map[string]interface{}{"color": "danger"}},
		},
	},
	{
		Code:        "industry",
		Name:        "行业类型",
		Description: "企业行业分类",
		Items: []SeedItem{
			{Value: "technology", Label: "科技", SortOrder: 1},
			{Value: "finance", Label: "金融", SortOrder: 2},
			{Value: "healthcare", Label: "医疗", SortOrder: 3},
			{Value: "education", Label: "教育", SortOrder: 4},
			{Value: "retail", Label: "零售", SortOrder: 5},
			{Value: "manufacturing", Label: "制造", SortOrder: 6},
			{Value: "other", Label: "其他", SortOrder: 7},
		},
	},
	{
		Code:        "user_type",
		Name:        "IAM用户类型",
		Description: "IAM用户类型",
		Items: []SeedItem{
			{Value: "api_key", Label: "API Key", SortOrder: 1},
			{Value: "access_key", Label: "Access Key", SortOrder: 2},
			{Value: "ram_user", Label: "RAM用户", SortOrder: 3},
			{Value: "iam_user", Label: "IAM用户", SortOrder: 4},
		},
	},
	{
		Code:        "user_status",
		Name:        "用户状态",
		Description: "用户状态",
		Items: []SeedItem{
			{Value: "active", Label: "活跃", SortOrder: 1, Extra: map[string]interface{}{"color": "success"}},
			{Value: "inactive", Label: "未激活", SortOrder: 2, Extra: map[string]interface{}{"color": "info"}},
			{Value: "deleted", Label: "已删除", SortOrder: 3, Extra: map[string]interface{}{"color": "danger"}},
		},
	},
	{
		Code:        "template_category",
		Name:        "策略模板分类",
		Description: "IAM策略模板分类",
		Items: []SeedItem{
			{Value: "read_only", Label: "只读权限", SortOrder: 1, Extra: map[string]interface{}{"description": "仅允许查看资源，不允许修改"}},
			{Value: "admin", Label: "管理员权限", SortOrder: 2, Extra: map[string]interface{}{"description": "拥有所有资源的完全访问权限"}},
			{Value: "developer", Label: "开发者权限", SortOrder: 3, Extra: map[string]interface{}{"description": "允许开发和测试环境的资源操作"}},
			{Value: "custom", Label: "自定义", SortOrder: 4, Extra: map[string]interface{}{"description": "用户自定义权限策略"}},
		},
	},
	{
		Code:        "sync_task_type",
		Name:        "同步任务类型",
		Description: "同步任务类型",
		Items: []SeedItem{
			{Value: "user_sync", Label: "用户同步", SortOrder: 1},
			{Value: "permission_sync", Label: "权限同步", SortOrder: 2},
			{Value: "group_sync", Label: "用户组同步", SortOrder: 3},
		},
	},
	{
		Code:        "sync_task_status",
		Name:        "同步任务状态",
		Description: "同步任务执行状态",
		Items: []SeedItem{
			{Value: "pending", Label: "等待中", SortOrder: 1, Extra: map[string]interface{}{"color": "info"}},
			{Value: "running", Label: "执行中", SortOrder: 2, Extra: map[string]interface{}{"color": "warning"}},
			{Value: "completed", Label: "已完成", SortOrder: 3, Extra: map[string]interface{}{"color": "success"}},
			{Value: "failed", Label: "失败", SortOrder: 4, Extra: map[string]interface{}{"color": "danger"}},
		},
	},
	{
		Code:        "operation_type",
		Name:        "操作类型",
		Description: "审计日志操作类型",
		Items: []SeedItem{
			{Value: "query", Label: "查询", SortOrder: 1, Extra: map[string]interface{}{"color": "info"}},
			{Value: "create", Label: "创建", SortOrder: 2, Extra: map[string]interface{}{"color": "success"}},
			{Value: "update", Label: "更新", SortOrder: 3, Extra: map[string]interface{}{"color": "warning"}},
			{Value: "delete", Label: "删除", SortOrder: 4, Extra: map[string]interface{}{"color": "danger"}},
			{Value: "sync", Label: "同步", SortOrder: 5, Extra: map[string]interface{}{"color": "info"}},
			{Value: "import", Label: "导入", SortOrder: 6, Extra: map[string]interface{}{"color": "info"}},
			{Value: "export", Label: "导出", SortOrder: 7, Extra: map[string]interface{}{"color": "info"}},
		},
	},
	{
		Code:        "target_type",
		Name:        "操作目标类型",
		Description: "操作目标资源类型",
		Items: []SeedItem{
			{Value: "user", Label: "用户", SortOrder: 1},
			{Value: "group", Label: "用户组", SortOrder: 2},
			{Value: "template", Label: "策略模板", SortOrder: 3},
			{Value: "policy", Label: "权限策略", SortOrder: 4},
		},
	},
	{
		Code:        "allocation_dimension",
		Name:        "分摊维度",
		Description: "成本分摊维度",
		Items: []SeedItem{
			{Value: "department", Label: "部门", SortOrder: 1},
			{Value: "resource_group", Label: "资源组", SortOrder: 2},
			{Value: "project", Label: "所属项目", SortOrder: 3},
			{Value: "tag", Label: "标签", SortOrder: 4},
		},
	},
	{
		Code:        "recommendation_type",
		Name:        "优化建议类型",
		Description: "成本优化建议类型",
		Items: []SeedItem{
			{Value: "downsize", Label: "降配建议", SortOrder: 1},
			{Value: "release_disk", Label: "释放云盘", SortOrder: 2},
			{Value: "convert_prepaid", Label: "转包年包月", SortOrder: 3},
			{Value: "idle_resource", Label: "闲置资源", SortOrder: 4},
		},
	},
	{
		Code:        "database_status",
		Name:        "数据库状态",
		Description: "数据库实例运行状态",
		Items: []SeedItem{
			{Value: "running", Label: "运行中", SortOrder: 1, Extra: map[string]interface{}{"color": "success"}},
			{Value: "stopped", Label: "已停止", SortOrder: 2, Extra: map[string]interface{}{"color": "info"}},
			{Value: "creating", Label: "创建中", SortOrder: 3, Extra: map[string]interface{}{"color": "warning"}},
			{Value: "deleting", Label: "删除中", SortOrder: 4, Extra: map[string]interface{}{"color": "warning"}},
			{Value: "error", Label: "异常", SortOrder: 5, Extra: map[string]interface{}{"color": "danger"}},
		},
	},
	{
		Code:        "region_group",
		Name:        "地区分组",
		Description: "地域分组",
		Items: []SeedItem{
			{Value: "cn_north", Label: "华北", SortOrder: 1},
			{Value: "cn_east", Label: "华东", SortOrder: 2},
			{Value: "cn_south", Label: "华南", SortOrder: 3},
			{Value: "cn_west", Label: "华西", SortOrder: 4},
			{Value: "cn_central", Label: "华中", SortOrder: 5},
			{Value: "overseas", Label: "海外", SortOrder: 6},
		},
	},
}

// defaultSeedTenantID 是种子数据落到的租户。
//
// 取值 2 的来历：迁移前这里用的是字符串 "default"，而这批既有字典数据在 eiam
// 侧对应的租户是 default-tenant，其数值 id 为 2（已对照 eiam 线上接口确认）。
// 故 int64(2) 是 "default" 的等价物，而不是任意挑选的数字。
//
// 注意不能用 0：按设计 §4.3，0 表示「会话未选定租户」，不是一个真实租户。
const defaultSeedTenantID int64 = 2

// SeedDictDataForAllTenants 为种子租户初始化字典数据。
//
// 原实现会先查 MongoDB 的 tenants 集合以枚举租户，该查询已删除，原因有两点：
//  1. 该集合从来不存在 —— 被删除的本地租户注册表用的集合名是 ecam_tenant。
//     Mongo 对不存在的集合执行 Find 返回空游标而非报错，故这个函数**从上线起
//     就一直返回空**，每次都走 "default" 回退分支。
//  2. 租户现在由 eiam 统一管理，本服务已无本地租户注册表可查。真要枚举全部
//     租户需调用 eiam 接口，那超出本次改动范围。
//
// 因此这里直接为 defaultSeedTenantID 播种，与迁移前的**实际**运行行为一致。
// db 参数保留以维持调用方签名不变。
func SeedDictDataForAllTenants(ctx context.Context, svc DictService, db *mongox.Mongo) (totalCreated, totalSkipped int, err error) {
	created, skipped, seedErr := SeedDictData(ctx, svc, defaultSeedTenantID)
	totalCreated += created
	totalSkipped += skipped
	if seedErr != nil {
		log.Printf("[WARN] seed: error seeding tenant %d: %v", defaultSeedTenantID, seedErr)
	}
	return totalCreated, totalSkipped, nil
}

// SeedDictData 初始化字典种子数据（幂等）
// 以字典类型 code 为幂等键，已存在则跳过
// 返回 (创建数量, 跳过数量, error)
func SeedDictData(ctx context.Context, svc DictService, tenantID int64) (created, skipped int, err error) {
	for _, st := range seedTypes {
		// 尝试创建类型，如果已存在则跳过
		dt, createErr := svc.CreateType(ctx, tenantID, CreateTypeReq{
			Code:        st.Code,
			Name:        st.Name,
			Description: st.Description,
		})
		if createErr != nil {
			if createErr == ErrDictTypeCodeExists {
				skipped++
				continue
			}
			log.Printf("[WARN] seed: failed to create dict type %s: %v", st.Code, createErr)
			continue
		}

		// 创建字典项
		for _, item := range st.Items {
			_, itemErr := svc.CreateItem(ctx, tenantID, dt.ID, CreateItemReq{
				Value:     item.Value,
				Label:     item.Label,
				SortOrder: item.SortOrder,
				Extra:     item.Extra,
			})
			if itemErr != nil {
				log.Printf("[WARN] seed: failed to create dict item %s/%s: %v", st.Code, item.Value, itemErr)
			}
		}
		created++
	}
	return created, skipped, nil
}

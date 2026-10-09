// Package port 定义 servicetree 模块对外部服务的依赖端口（六边形架构）。
// 通过端口抽象解耦 CAM 与 CMDB 的直接依赖，支持本地调用和远程调用两种实现。
package port

import (
	"context"
	"time"
)

// CMDBInstance CMDB 实例的精简视图（仅 servicetree 需要的字段）
// 与 cmdb/domain.Instance 解耦，避免跨模块依赖
type CMDBInstance struct {
	ID         int64
	ModelUID   string                 // 模型UID (如 aliyun_ecs, cloud_vm)
	AssetID    string                 // 云厂商资产ID
	AssetName  string                 // 资产名称
	TenantID   int64                  // 租户ID
	AccountID  int64                  // 云账号ID
	Attributes map[string]interface{} // 动态属性
	CreateTime time.Time
	UpdateTime time.Time
}

// GetStringAttribute 获取字符串类型属性
func (i *CMDBInstance) GetStringAttribute(key string) string {
	if i.Attributes == nil {
		return ""
	}
	if val, ok := i.Attributes[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// AssetStatsResult 资产统计结果
type AssetStatsResult struct {
	Total       int64
	ByAssetType []AssetTypeCount
	ByProvider  []ProviderCount
}

// AssetTypeCount 按资产类型统计
type AssetTypeCount struct {
	AssetType string
	Count     int64
}

// ProviderCount 按云厂商统计
type ProviderCount struct {
	Provider string
	Count    int64
}

// CMDBPort CMDB 服务端口接口
// servicetree 模块通过此接口访问 CMDB 数据，不直接依赖 cmdb 包
type CMDBPort interface {
	// ListByIDs 根据 ID 列表批量查询实例
	ListByIDs(ctx context.Context, ids []int64) ([]CMDBInstance, error)

	// ListUnbound 查询未绑定到服务树的资产
	ListUnbound(ctx context.Context, tenantID int64, offset, limit int64) ([]CMDBInstance, error)

	// CountUnbound 统计未绑定资产数量
	CountUnbound(ctx context.Context, tenantID int64) (int64, error)

	// AggregateStatsByIDs 根据资源ID列表聚合统计
	AggregateStatsByIDs(ctx context.Context, ids []int64) (*AssetStatsResult, error)

	// AggregateAllStats 聚合统计全部资产
	AggregateAllStats(ctx context.Context, tenantID int64) (*AssetStatsResult, error)

	// AggregateUnboundStats 聚合统计未绑定资产
	AggregateUnboundStats(ctx context.Context, tenantID int64) (*AssetStatsResult, error)
}

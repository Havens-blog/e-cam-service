package repository

import (
	"go.mongodb.org/mongo-driver/bson"
)

// TenantFilter 租户过滤器辅助函数
// 用于在所有查询中自动添加租户隔离条件

// WithTenantID 为查询条件添加租户ID过滤
func WithTenantID(filter bson.M, tenantID string) bson.M {
	if tenantID != "" {
		filter["tenant_id"] = tenantID
	}
	return filter
}

// BuildTenantFilter 构建包含租户ID的基础过滤器
func BuildTenantFilter(tenantID string) bson.M {
	return WithTenantID(bson.M{}, tenantID)
}

// MergeTenantFilter 合并租户过滤器到现有过滤器
func MergeTenantFilter(existingFilter bson.M, tenantID string) bson.M {
	if existingFilter == nil {
		existingFilter = bson.M{}
	}
	return WithTenantID(existingFilter, tenantID)
}

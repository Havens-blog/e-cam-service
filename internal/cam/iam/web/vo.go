package web

import (
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// CreateUserVO 创建用户请求VO
type CreateUserVO struct {
	Username       string               `json:"username" binding:"required,min=1,max=100"`
	UserType       domain.CloudUserType `json:"user_type" binding:"required"`
	CloudAccountID int64                `json:"cloud_account_id" binding:"required"`
	DisplayName    string               `json:"display_name" binding:"max=200"`
	Email          string               `json:"email" binding:"omitempty,email"`
	UserGroups     []int64              `json:"user_groups"` // 用户组ID列表
	TenantID       string               `json:"tenant_id" binding:"required"`
}

// UpdateUserVO 更新用户请求VO
type UpdateUserVO struct {
	DisplayName *string                 `json:"display_name,omitempty"`
	Email       *string                 `json:"email,omitempty"`
	UserGroups  []int64                 `json:"user_groups,omitempty"` // 用户组ID列表
	Status      *domain.CloudUserStatus `json:"status,omitempty"`
}

// ListUsersVO 查询用户列表请求VO
type ListUsersVO struct {
	Provider       domain.CloudProvider   `json:"provider" form:"provider"`
	UserType       domain.CloudUserType   `json:"user_type" form:"user_type"`
	Status         domain.CloudUserStatus `json:"status" form:"status"`
	CloudAccountID int64                  `json:"cloud_account_id" form:"cloud_account_id"`
	TenantID       string                 `json:"tenant_id" form:"tenant_id"`
	Keyword        string                 `json:"keyword" form:"keyword"`
	Page           int64                  `json:"page" form:"page"`
	Size           int64                  `json:"size" form:"size"`
}

// AssignUserGroupsVO 批量分配用户组请求VO
type AssignUserGroupsVO struct {
	UserIDs  []int64 `json:"user_ids" binding:"required,min=1"`
	GroupIDs []int64 `json:"group_ids" binding:"required,min=1"`
}

// CreateUserGroupVO 创建用户组请求VO
type CreateUserGroupVO struct {
	Name           string                    `json:"name" binding:"required,min=1,max=100"`
	Description    string                    `json:"description" binding:"max=500"`
	Policies       []domain.PermissionPolicy `json:"policies"`
	CloudPlatforms []domain.CloudProvider    `json:"cloud_platforms" binding:"required,min=1"`
	TenantID       string                    `json:"tenant_id" binding:"required"`
}

// UpdateUserGroupVO 更新用户组请求VO
type UpdateUserGroupVO struct {
	Name           *string                   `json:"name,omitempty"`
	Description    *string                   `json:"description,omitempty"`
	Policies       []domain.PermissionPolicy `json:"policies,omitempty"`
	CloudPlatforms []domain.CloudProvider    `json:"cloud_platforms,omitempty"`
}

// ListUserGroupsVO 查询用户组列表请求VO
type ListUserGroupsVO struct {
	TenantID string `json:"tenant_id" form:"tenant_id"`
	Keyword  string `json:"keyword" form:"keyword"`
	Page     int64  `json:"page" form:"page"`
	Size     int64  `json:"size" form:"size"`
}

// CreateSyncTaskVO 创建同步任务请求VO
type CreateSyncTaskVO struct {
	TaskType       domain.SyncTaskType   `json:"task_type" binding:"required"`
	TargetType     domain.SyncTargetType `json:"target_type" binding:"required"`
	TargetID       int64                 `json:"target_id" binding:"required"`
	CloudAccountID int64                 `json:"cloud_account_id" binding:"required"`
	Provider       domain.CloudProvider  `json:"provider" binding:"required"`
}

// ListSyncTasksVO 查询同步任务列表请求VO
type ListSyncTasksVO struct {
	TaskType       domain.SyncTaskType   `json:"task_type" form:"task_type"`
	Status         domain.SyncTaskStatus `json:"status" form:"status"`
	CloudAccountID int64                 `json:"cloud_account_id" form:"cloud_account_id"`
	Provider       domain.CloudProvider  `json:"provider" form:"provider"`
	Page           int64                 `json:"page" form:"page"`
	Size           int64                 `json:"size" form:"size"`
}

// ListAuditLogsVO 查询审计日志列表请求VO
type ListAuditLogsVO struct {
	OperationType domain.AuditOperationType `json:"operation_type" form:"operation_type"`
	OperatorID    string                    `json:"operator_id" form:"operator_id"`
	TargetType    string                    `json:"target_type" form:"target_type"`
	CloudPlatform domain.CloudProvider      `json:"cloud_platform" form:"cloud_platform"`
	TenantID      string                    `json:"tenant_id" form:"tenant_id"`
	StartTime     string                    `json:"start_time" form:"start_time"`
	EndTime       string                    `json:"end_time" form:"end_time"`
	Page          int64                     `json:"page" form:"page"`
	Size          int64                     `json:"size" form:"size"`
}

// ExportAuditLogsVO 导出审计日志请求VO
type ExportAuditLogsVO struct {
	OperationType domain.AuditOperationType `json:"operation_type" form:"operation_type"`
	OperatorID    string                    `json:"operator_id" form:"operator_id"`
	TargetType    string                    `json:"target_type" form:"target_type"`
	CloudPlatform domain.CloudProvider      `json:"cloud_platform" form:"cloud_platform"`
	TenantID      string                    `json:"tenant_id" form:"tenant_id"`
	StartTime     string                    `json:"start_time" form:"start_time"`
	EndTime       string                    `json:"end_time" form:"end_time"`
	Format        domain.ExportFormat       `json:"format" form:"format" binding:"required"`
}

// GenerateAuditReportVO 生成审计报告请求VO
type GenerateAuditReportVO struct {
	StartTime string `json:"start_time" binding:"required"`
	EndTime   string `json:"end_time" binding:"required"`
	TenantID  string `json:"tenant_id" binding:"required"`
}

// CreateTemplateVO 创建策略模板请求VO
type CreateTemplateVO struct {
	Name           string                    `json:"name" binding:"required,min=1,max=100"`
	Description    string                    `json:"description" binding:"max=500"`
	Category       domain.TemplateCategory   `json:"category" binding:"required"`
	Policies       []domain.PermissionPolicy `json:"policies"`
	CloudPlatforms []domain.CloudProvider    `json:"cloud_platforms" binding:"required,min=1"`
	TenantID       string                    `json:"tenant_id" binding:"required"`
}

// UpdateTemplateVO 更新策略模板请求VO
type UpdateTemplateVO struct {
	Name           *string                   `json:"name,omitempty"`
	Description    *string                   `json:"description,omitempty"`
	Policies       []domain.PermissionPolicy `json:"policies,omitempty"`
	CloudPlatforms []domain.CloudProvider    `json:"cloud_platforms,omitempty"`
}

// ListTemplatesVO 查询策略模板列表请求VO
type ListTemplatesVO struct {
	Category  domain.TemplateCategory `json:"category" form:"category"`
	IsBuiltIn *bool                   `json:"is_built_in" form:"is_built_in"`
	TenantID  string                  `json:"tenant_id" form:"tenant_id"`
	Keyword   string                  `json:"keyword" form:"keyword"`
	Page      int64                   `json:"page" form:"page"`
	Size      int64                   `json:"size" form:"size"`
}

// CreateFromGroupVO 从用户组创建模板请求VO
type CreateFromGroupVO struct {
	GroupID     int64  `json:"group_id" binding:"required"`
	Name        string `json:"name" binding:"required,min=1,max=100"`
	Description string `json:"description" binding:"max=500"`
}

// UpdatePoliciesVO 更新权限策略请求VO
type UpdatePoliciesVO struct {
	Policies []domain.PermissionPolicy `json:"policies" binding:"required,min=1"`
}

// Tenant related VOs

// CreateTenantVO 创建租户请求VO
type CreateTenantVO struct {
	ID          string                 `json:"id" binding:"required,min=1,max=50"`
	Name        string                 `json:"name" binding:"required,min=1,max=100"`
	DisplayName string                 `json:"display_name" binding:"max=200"`
	Description string                 `json:"description" binding:"max=500"`
	Settings    domain.TenantSettings  `json:"settings"`
	Metadata    domain.TenantMetadata  `json:"metadata"`
}

// UpdateTenantVO 更新租户请求VO
type UpdateTenantVO struct {
	Name        *string                `json:"name,omitempty"`
	DisplayName *string                `json:"display_name,omitempty"`
	Description *string                `json:"description,omitempty"`
	Status      *domain.TenantStatus   `json:"status,omitempty"`
	Settings    *domain.TenantSettings `json:"settings,omitempty"`
	Metadata    *domain.TenantMetadata `json:"metadata,omitempty"`
}

// ListTenantsVO 查询租户列表请求VO
type ListTenantsVO struct {
	Keyword  string `json:"keyword" form:"keyword"`
	Status   string `json:"status" form:"status"`
	Industry string `json:"industry" form:"industry"`
	Region   string `json:"region" form:"region"`
	Page     int64  `json:"page" form:"page"`
	Size     int64  `json:"size" form:"size"`
}

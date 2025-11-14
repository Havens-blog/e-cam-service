package domain

import (
	"fmt"
	"time"
)

// CloudUserType 用户类型
type CloudUserType string

const (
	CloudUserTypeAPIKey    CloudUserType = "api_key"
	CloudUserTypeAccessKey CloudUserType = "access_key"
	CloudUserTypeRAMUser   CloudUserType = "ram_user"
	CloudUserTypeIAMUser   CloudUserType = "iam_user"
)

// CloudUserStatus 用户状态
type CloudUserStatus string

const (
	CloudUserStatusActive   CloudUserStatus = "active"
	CloudUserStatusInactive CloudUserStatus = "inactive"
	CloudUserStatusDeleted  CloudUserStatus = "deleted"
)

// CloudUserMetadata 用户元数据
type CloudUserMetadata struct {
	LastLoginTime   *time.Time        `json:"last_login_time" bson:"last_login_time"`
	LastSyncTime    *time.Time        `json:"last_sync_time" bson:"last_sync_time"`
	AccessKeyCount  int               `json:"access_key_count" bson:"access_key_count"`
	MFAEnabled      bool              `json:"mfa_enabled" bson:"mfa_enabled"`
	PasswordLastSet *time.Time        `json:"password_last_set" bson:"password_last_set"`
	Tags            map[string]string `json:"tags" bson:"tags"`
}

// CloudUser 云平台用户领域模型
type CloudUser struct {
	ID               int64             `json:"id" bson:"id"`
	Username         string            `json:"username" bson:"username"`
	UserType         CloudUserType     `json:"user_type" bson:"user_type"`
	CloudAccountID   int64             `json:"cloud_account_id" bson:"cloud_account_id"`
	Provider         CloudProvider     `json:"provider" bson:"provider"`
	CloudUserID      string            `json:"cloud_user_id" bson:"cloud_user_id"`
	DisplayName      string            `json:"display_name" bson:"display_name"`
	Email            string            `json:"email" bson:"email"`
	PermissionGroups []int64           `json:"permission_groups" bson:"permission_groups"`
	Metadata         CloudUserMetadata `json:"metadata" bson:"metadata"`
	Status           CloudUserStatus   `json:"status" bson:"status"`
	TenantID         string            `json:"tenant_id" bson:"tenant_id"`
	CreateTime       time.Time         `json:"create_time" bson:"create_time"`
	UpdateTime       time.Time         `json:"update_time" bson:"update_time"`
	CTime            int64             `json:"ctime" bson:"ctime"`
	UTime            int64             `json:"utime" bson:"utime"`
}


// CloudUserFilter 云用户查询过滤器
type CloudUserFilter struct {
	Provider       CloudProvider   `json:"provider"`
	UserType       CloudUserType   `json:"user_type"`
	Status         CloudUserStatus `json:"status"`
	CloudAccountID int64           `json:"cloud_account_id"`
	TenantID       string          `json:"tenant_id"`
	Keyword        string          `json:"keyword"`
	Offset         int64           `json:"offset"`
	Limit          int64           `json:"limit"`
}

// CreateCloudUserRequest 创建云用户请求
type CreateCloudUserRequest struct {
	Username         string            `json:"username" binding:"required,min=1,max=100"`
	UserType         CloudUserType     `json:"user_type" binding:"required"`
	CloudAccountID   int64             `json:"cloud_account_id" binding:"required"`
	DisplayName      string            `json:"display_name" binding:"max=200"`
	Email            string            `json:"email" binding:"omitempty,email"`
	PermissionGroups []int64           `json:"permission_groups"`
	TenantID         string            `json:"tenant_id" binding:"required"`
}

// UpdateCloudUserRequest 更新云用户请求
type UpdateCloudUserRequest struct {
	DisplayName      *string `json:"display_name,omitempty"`
	Email            *string `json:"email,omitempty"`
	PermissionGroups []int64 `json:"permission_groups,omitempty"`
	Status           *CloudUserStatus `json:"status,omitempty"`
}

// 领域方法

// IsActive 判断用户是否为活跃状态
func (u *CloudUser) IsActive() bool {
	return u.Status == CloudUserStatusActive
}

// Validate 验证云用户数据
func (u *CloudUser) Validate() error {
	if u.Username == "" {
		return fmt.Errorf("username cannot be empty")
	}
	if u.UserType == "" {
		return fmt.Errorf("user type cannot be empty")
	}
	if u.CloudAccountID == 0 {
		return fmt.Errorf("cloud account id cannot be empty")
	}
	if u.Provider == "" {
		return fmt.Errorf("provider cannot be empty")
	}
	if u.TenantID == "" {
		return fmt.Errorf("tenant id cannot be empty")
	}
	return nil
}

// UpdateMetadata 更新用户元数据
func (u *CloudUser) UpdateMetadata(metadata CloudUserMetadata) {
	u.Metadata = metadata
	u.UpdateTime = time.Now()
	u.UTime = u.UpdateTime.Unix()
}

package aliyun

import (
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ram"
)

// ConvertRAMUserToCloudUser 转换RAM用户为CloudUser领域模型
func ConvertRAMUserToCloudUser(ramUser ram.User, account *domain.CloudAccount) *domain.CloudUser {
	now := time.Now()

	// 解析创建时间
	createTime := now
	if ramUser.CreateDate != "" {
		if t, err := time.Parse(time.RFC3339, ramUser.CreateDate); err == nil {
			createTime = t
		}
	}

	// 解析最后登录时间
	var lastLoginTime *time.Time
	if ramUser.LastLoginDate != "" {
		if t, err := time.Parse(time.RFC3339, ramUser.LastLoginDate); err == nil {
			lastLoginTime = &t
		}
	}

	user := &domain.CloudUser{
		Username:       ramUser.UserName,
		UserType:       domain.CloudUserTypeRAMUser,
		CloudAccountID: account.ID,
		Provider:       domain.CloudProviderAliyun,
		CloudUserID:    ramUser.UserId,
		DisplayName:    ramUser.DisplayName,
		Email:          ramUser.Email,
		Status:         domain.CloudUserStatusActive,
		TenantID:       account.TenantID,
		CreateTime:     createTime,
		UpdateTime:     now,
		CTime:          createTime.Unix(),
		UTime:          now.Unix(),
		Metadata: domain.CloudUserMetadata{
			LastLoginTime: lastLoginTime,
			LastSyncTime:  &now,
			Tags:          make(map[string]string),
		},
	}

	return user
}

// ConvertPolicyType 转换策略类型
func ConvertPolicyType(ramPolicyType string) domain.PolicyType {
	switch ramPolicyType {
	case "System":
		return domain.PolicyTypeSystem
	case "Custom":
		return domain.PolicyTypeCustom
	default:
		return domain.PolicyTypeCustom
	}
}

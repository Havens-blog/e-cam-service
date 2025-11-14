package aws

import (
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// ConvertIAMUserToCloudUser 转换 IAM 用户为 CloudUser 领域模型
func ConvertIAMUserToCloudUser(iamUser types.User, account *domain.CloudAccount) *domain.CloudUser {
	now := time.Now()

	// 解析创建时间
	createTime := now
	if iamUser.CreateDate != nil {
		createTime = *iamUser.CreateDate
	}

	// 解析最后密码使用时间
	var passwordLastSet *time.Time
	if iamUser.PasswordLastUsed != nil {
		passwordLastSet = iamUser.PasswordLastUsed
	}

	user := &domain.CloudUser{
		Username:       *iamUser.UserName,
		UserType:       domain.CloudUserTypeIAMUser,
		CloudAccountID: account.ID,
		Provider:       domain.CloudProviderAWS,
		CloudUserID:    *iamUser.UserId,
		Status:         domain.CloudUserStatusActive,
		TenantID:       account.TenantID,
		CreateTime:     createTime,
		UpdateTime:     now,
		CTime:          createTime.Unix(),
		UTime:          now.Unix(),
		Metadata: domain.CloudUserMetadata{
			PasswordLastSet: passwordLastSet,
			LastSyncTime:    &now,
			Tags:            convertTags(iamUser.Tags),
		},
	}

	return user
}

// ConvertPolicyScope 转换策略范围为策略类型
func ConvertPolicyScope(policyArn *string) domain.PolicyType {
	if policyArn == nil {
		return domain.PolicyTypeCustom
	}

	// AWS 托管策略的 ARN 格式: arn:aws:iam::aws:policy/...
	// 客户托管策略的 ARN 格式: arn:aws:iam::123456789012:policy/...
	if strings.Contains(*policyArn, ":iam::aws:policy/") {
		return domain.PolicyTypeSystem
	}

	return domain.PolicyTypeCustom
}

// convertTags 转换 AWS 标签为 map
func convertTags(tags []types.Tag) map[string]string {
	result := make(map[string]string)
	for _, tag := range tags {
		if tag.Key != nil && tag.Value != nil {
			result[*tag.Key] = *tag.Value
		}
	}
	return result
}

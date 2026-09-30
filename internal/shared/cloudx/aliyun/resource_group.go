package aliyun

import (
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/resourcemanager"
)

// ListResourceGroupNames 拉取阿里云账号下全部资源组，返回 ID -> 名称 映射。
// 资源组为账号级（不区分地域），数量通常很少（<100），单页 100 兜底翻页。
// 名称优先取 Name，为空时降级 DisplayName；结果供资产同步写入 attributes.resource_group_name。
func ListResourceGroupNames(account *domain.CloudAccount) (map[string]string, error) {
	if account == nil {
		return nil, fmt.Errorf("账号不能为空")
	}

	client, err := resourcemanager.NewClientWithAccessKey("cn-hangzhou", account.AccessKeyID, account.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("创建资源管理器客户端失败: %w", err)
	}

	result := make(map[string]string)
	request := resourcemanager.CreateListResourceGroupsRequest()
	request.PageSize = requests.NewInteger(100)

	for page := 1; ; page++ {
		request.PageNumber = requests.NewInteger(page)
		resp, err := client.ListResourceGroups(request)
		if err != nil {
			return nil, fmt.Errorf("列出阿里云资源组失败: %w", err)
		}

		groups := resp.ResourceGroups.ResourceGroup
		for _, rg := range groups {
			name := rg.Name
			if name == "" {
				name = rg.DisplayName
			}
			if rg.Id != "" && name != "" {
				result[rg.Id] = name
			}
		}

		if len(groups) < 100 || page*100 >= resp.TotalCount {
			break
		}
	}

	return result, nil
}
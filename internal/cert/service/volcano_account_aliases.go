package service

import (
	sharedomain "github.com/Havens-blog/e-cloudx-sdk/domain"
)

// volcanoAccountProviders 火山双别名归一：cloudx/volcano 以 volcano/volcengine
// 两个 provider 标识注册同一适配器（shared/domain 常量并列，见
// cloudx/volcano/adapter.go init），账号登记可能以任一名存在——查询任一别名
// 均须返回全部火山账号（调用方按账号名去重）。返回该云应查询的 provider
// 清单；其余云保持单 provider 过滤（行为不变）。
func volcanoAccountProviders(cloud string) []sharedomain.CloudProvider {
	if cloud == string(sharedomain.CloudProviderVolcano) || cloud == string(sharedomain.CloudProviderVolcengine) {
		return []sharedomain.CloudProvider{
			sharedomain.CloudProviderVolcano,
			sharedomain.CloudProviderVolcengine,
		}
	}
	return []sharedomain.CloudProvider{sharedomain.CloudProvider(cloud)}
}

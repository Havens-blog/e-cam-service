package executor

import (
	"context"
	"fmt"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/gotomicro/ego/core/elog"
)

// syncRegionWAF 同步单个地域的 WAF 实例
func (e *SyncAssetsExecutor) syncRegionWAF(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_waf", account.Provider)

	// 腾讯云 WAF/TEO 是全局服务:任何地域调用都返回同一份全量列表(转换层把 Region
	// 硬编码为 ap-guangzhou)。若沿账号×地域循环每个 region 各执行一次,会产生
	// K×全量的 API 请求风暴(列表 + 每域名详情 N+1,K≈地域数),极易触发腾讯限流;
	// 且一旦某一轮 WAF 列表接口报错,ListInstancesWithFilter 只告警不阻断地返回
	// 空列表,diffAndUpsert 会把本地已存的全部腾讯 WAF 行当过期删除——列表出现
	// "时有时无/缺行"。故与 DNS(账号级全局服务)同理,只在 canonical 地域
	// ap-guangzhou 执行一次,其余地域直接跳过。
	if account.Provider == domain.CloudProviderTencent && region != "ap-guangzhou" {
		return 0, nil
	}

	wafAdapter := adapter.WAF()
	if wafAdapter == nil {
		return 0, fmt.Errorf("WAF适配器不可用")
	}

	cloudInstances, err := wafAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取WAF实例列表失败: %w", err)
	}

	items := make([]syncItem, 0, len(cloudInstances))
	for _, inst := range cloudInstances {
		items = append(items, syncItem{
			AssetID: inst.InstanceID,
			ToInstance: func() (camdomain.Instance, error) {
				return e.convertWAFToInstance(inst, account), nil
			},
		})
	}

	synced, deleted, err := e.diffAndUpsert(ctx, account.TenantID, modelUID, account.ID, region, items)
	if err != nil {
		return synced, err
	}

	e.logger.Info("同步地域WAF完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int64("deleted", deleted))

	return synced, nil
}

// convertWAFToInstance 将 WAF 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertWAFToInstance(inst types.WAFInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_waf", account.Provider)

	attributes := map[string]any{
		"status":      inst.Status,
		"region":      inst.Region,
		"provider":    inst.Provider,
		"description": inst.Description,
		"edition":     inst.Edition,

		"domain_count":    inst.DomainCount,
		"domain_limit":    inst.DomainLimit,
		"protected_hosts": inst.ProtectedHosts,
		"source_ips":      inst.SourceIPs,
		"cname":           inst.Cname,

		"rule_count":       inst.RuleCount,
		"acl_rule_count":   inst.ACLRuleCount,
		"cc_rule_count":    inst.CCRuleCount,
		"rate_limit_count": inst.RateLimitCount,
		"protection_mode":  inst.ProtectionMode,
		"rules":            inst.Rules,

		"waf_enabled":      inst.WAFEnabled,
		"cc_enabled":       inst.CCEnabled,
		"anti_bot_enabled": inst.AntiBotEnabled,

		"qps":          inst.QPS,
		"bandwidth":    inst.Bandwidth,
		"exclusive_ip": inst.ExclusiveIP,
		"pay_type":     inst.PayType,

		"creation_time": inst.CreationTime,
		"expired_time":  inst.ExpiredTime,

		"project_id":        inst.ProjectID,
		"resource_group_id": inst.ResourceGroupID,

		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,
		"tags":               inst.Tags,
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    inst.InstanceID,
		AssetName:  inst.InstanceName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}

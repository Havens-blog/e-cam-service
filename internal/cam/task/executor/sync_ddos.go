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

// syncRegionDDOS 同步单个地域的 DDoS 防护实例。
//
// 全局服务守卫:腾讯 antiddos(高防IP/高防包)、华为 AAD、AWS Shield 都是账号级
// 全局服务,任何地域调用返回同一份全量列表。若沿账号×地域循环每个 region 各执行
// 一次,会产生 K×全量的 API 请求风暴(K≈地域数),极易触发厂商限流;一旦某轮列表
// 接口报错,diffAndUpsert 会把本地已存的行当过期删除,列表出现"时有时无/缺行"
// (与 sync_waf.go 中腾讯 WAF 的守卫同一根因)。故:
//   - 腾讯:仅在 canonical 地域执行一次。canonical 优先 ap-guangzhou(与 WAF
//     口径一致);若本任务循环的实际地域列表不含 ap-guangzhou(任务参数过滤),
//     退回循环首个地域——否则整组会被跳过,同步 0 行 0 错误。
//     注意:腾讯转换层 Region 装实例自带的真实地域(tencent/ddos.go 的
//     Region.Region → Region,空则 ap-guangzhou),因此删除 diff 只能清理
//     attributes.region 恰为 canonical 地域的行;真实地域非 canonical 的
//     行 add/update 恒正确,仅云端删除后本地残留一行,属已知可接受窗口;
//   - 华为/AWS:仅在"本任务循环首个地域"执行一次(canonical 来自任务循环注入
//     ctx 的地域序列,而非 account.Regions[0]——两者顺序可能不同,不一致会令
//     整组被跳过);
//   - 阿里云 DDoS 原生防护(ddosbgp)按地域过滤,属区域性资源,不设守卫,
//     沿地域循环正常执行(高防 ddoscoo 结果为全局列表,按同一 asset_id 幂等
//     upsert,不会产生重复行)。
func (e *SyncAssetsExecutor) syncRegionDDOS(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_ddos", account.Provider)

	// canonical 地域以"本任务循环实际同步的地域列表"(GetRegions 经 params.Regions
	// 过滤,任务循环经 ctx 注入)为准;ctx 缺失时(直接调用/单测)回退旧口径。
	loopRegions := syncLoopRegionIDsFromCtx(ctx)

	switch account.Provider {
	case domain.CloudProviderTencent:
		canonical := "ap-guangzhou"
		if len(loopRegions) > 0 {
			hasGZ := false
			for _, r := range loopRegions {
				if r == "ap-guangzhou" {
					hasGZ = true
					break
				}
			}
			if !hasGZ {
				canonical = loopRegions[0]
			}
		}
		if region != canonical {
			return 0, nil
		}
	case domain.CloudProviderHuawei, domain.CloudProviderAWS:
		canonical := ""
		if len(loopRegions) > 0 {
			canonical = loopRegions[0]
		} else if len(account.Regions) > 0 {
			canonical = account.Regions[0]
		}
		if canonical != "" && region != canonical {
			return 0, nil
		}
	}

	ddosAdapter := adapter.DDOS()
	if ddosAdapter == nil {
		// 厂商 SDK 未提供 DDoS 实例枚举能力(如火山引擎 advdefence 仅打包转
		// 发规则/攻击统计等接口,无高防实例列表 API)。视为"该厂商未接入",
		// 静默跳过而非报错,避免每次同步刷错误日志。
		e.logger.Info("该厂商暂未接入DDoS防护同步(适配器不可用)",
			elog.String("provider", string(account.Provider)))
		return 0, nil
	}

	cloudInstances, err := ddosAdapter.ListInstances(ctx, region)
	if err != nil {
		// fail-fast:不返回部分列表,防止 diffAndUpsert 把未拉到的那部分产品线
		// 本地行当过期删除(与 tencent/waf.go 的删除级联防护一致)。
		return 0, fmt.Errorf("获取DDoS防护实例列表失败: %w", err)
	}

	items := make([]syncItem, 0, len(cloudInstances))
	for _, inst := range cloudInstances {
		if inst.InstanceID == "" {
			e.logger.Warn("跳过缺失实例ID的DDoS条目",
				elog.String("provider", string(account.Provider)),
				elog.String("name", inst.InstanceName))
			continue
		}
		items = append(items, syncItem{
			AssetID: inst.InstanceID,
			ToInstance: func() (camdomain.Instance, error) {
				return e.convertDDOSToInstance(inst, account), nil
			},
		})
	}

	synced, deleted, err := e.diffAndUpsert(ctx, account.TenantID, modelUID, account.ID, region, items)
	if err != nil {
		return synced, err
	}

	e.logger.Info("同步地域DDoS完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int64("deleted", deleted))

	return synced, nil
}

// convertDDOSToInstance 将 DDoS 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertDDOSToInstance(inst types.DDOSInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_ddos", account.Provider)

	// 备注:bandwidth_unit 为厂商 SDK 注释实证的带宽单位(如腾讯 BGP 高防包
	// 注释明确 Gbps、BGPIP 注释明确 Mbps);华为 AAD 未标注单位,转换层不臆断,
	// 字段留空由展示层只显示原始数值。
	attributes := map[string]any{
		"status":    inst.Status,
		"region":    inst.Region,
		"provider":  inst.Provider,
		"edition":   inst.Edition,

		"basic_bandwidth":    inst.BasicBandwidth,
		"elastic_bandwidth":  inst.ElasticBandwidth,
		"service_bandwidth":  inst.ServiceBandwidth,
		"bandwidth_unit":     inst.BandwidthUnit,
		"cc_qps":             inst.CCQPS,
		"protected_ips":      inst.ProtectedIPs,
		"protected_ip_count": inst.ProtectedIPCount,

		"charge_type":   inst.ChargeType,
		"auto_renew":    inst.AutoRenew,
		"creation_time": inst.CreationTime,
		"expired_time":  inst.ExpiredTime,

		"project_id":        inst.ProjectID,
		"resource_group_id": inst.ResourceGroupID,

		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,
		"tags":               inst.Tags,
		"description":        inst.Description,
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
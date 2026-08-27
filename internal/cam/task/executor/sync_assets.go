// Package executor 同步资产任务执行器（异步任务队列，生产环境主入口）
//
// 文件：internal/cam/task/executor/sync_assets.go
//
// 作用：实现 SyncAssetsExecutor，由异步任务队列（定时任务/手动触发）调度执行，
//
//	通过 wire 注入到运行时。负责按账号、地域遍历云资产并写入 CMDB c_instance 表，
//	包含完整的"获取云端列表 → 对比本地 → 删除过期 → Upsert 新增/更新"清理逻辑。
//
// 与其他同步文件的关系：
//   - internal/cam/service/asset_sync.go          ← API 直接调用的同步服务（AssetSyncService）。
//   - internal/task/executor/sync_assets.go       ← 旧版/备用执行器，不参与运行时。
//   - 本文件（sync_assets.go）                     ← 生产环境实际运行的任务执行器。
//
// 注意：两条同步路径写入同一个 c_instance 集合，model_uid 必须保持一致，
//
//	统一使用 fmt.Sprintf("%s_xxx", account.Provider) 格式（如 aliyun_ecs）。
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/asset"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/mongo"
)

// 定义任务类型常量
const (
	TaskTypeSyncAssets taskx.TaskType = "cam:sync_assets"
)

// SyncAssetsExecutor 同步资产任务执行器
type SyncAssetsExecutor struct {
	accountRepo    repository.CloudAccountRepository
	instanceRepo   repository.InstanceRepository
	adapterFactory *asset.AdapterFactory
	cloudxFactory  *cloudx.AdapterFactory
	taskRepo       taskx.TaskRepository
	dnsDomainColl  *mongo.Collection // DNS 域名集合 (c_dns_domain)
	dnsRecordColl  *mongo.Collection // DNS 记录集合 (c_dns_record)
	logger         *elog.Component
}

// NewSyncAssetsExecutor 创建同步资产任务执行器
func NewSyncAssetsExecutor(
	accountRepo repository.CloudAccountRepository,
	instanceRepo repository.InstanceRepository,
	adapterFactory *asset.AdapterFactory,
	taskRepo taskx.TaskRepository,
	logger *elog.Component,
) *SyncAssetsExecutor {
	return &SyncAssetsExecutor{
		accountRepo:    accountRepo,
		instanceRepo:   instanceRepo,
		adapterFactory: adapterFactory,
		cloudxFactory:  cloudx.NewAdapterFactory(logger),
		taskRepo:       taskRepo,
		logger:         logger,
	}
}

// GetType 获取任务类型
func (e *SyncAssetsExecutor) GetType() taskx.TaskType {
	return TaskTypeSyncAssets
}

// SetDNSCollections 设置 DNS 专用集合（可选注入）
func (e *SyncAssetsExecutor) SetDNSCollections(domainColl, recordColl *mongo.Collection) {
	e.dnsDomainColl = domainColl
	e.dnsRecordColl = recordColl
}

// Execute 执行任务
func (e *SyncAssetsExecutor) Execute(ctx context.Context, t *taskx.Task) error {
	e.logger.Info("开始执行同步资产任务", elog.String("task_id", t.ID))

	// 解析任务参数
	var params SyncAssetsParams
	paramsBytes, err := json.Marshal(t.Params)
	if err != nil {
		return fmt.Errorf("序列化任务参数失败: %w", err)
	}
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		return fmt.Errorf("解析任务参数失败: %w", err)
	}

	e.logger.Info("任务参数",
		elog.Int64("account_id", params.AccountID),
		elog.Any("asset_types", params.AssetTypes))

	// 如果未指定资源类型，默认同步所有支持的类型
	if len(params.AssetTypes) == 0 {
		params.AssetTypes = domain.DefaultSyncAssetTypes
	}

	// 更新进度: 开始同步
	e.taskRepo.UpdateProgress(ctx, t.ID, 10, "正在获取云账号信息")

	// 获取需要同步的账号列表
	var accounts []domain.CloudAccount
	if params.AccountID > 0 {
		// 指定了账号ID，同步单个账号
		account, err := e.accountRepo.GetByID(ctx, params.AccountID)
		if err != nil {
			return fmt.Errorf("获取云账号失败: %w", err)
		}
		accounts = append(accounts, account)
	} else {
		// 未指定账号ID，查询该云厂商的所有活跃账号
		filter := domain.CloudAccountFilter{
			Provider: domain.CloudProvider(params.Provider),
			Status:   domain.CloudAccountStatusActive,
			Limit:    100,
		}
		accts, _, err := e.accountRepo.List(ctx, filter)
		if err != nil {
			return fmt.Errorf("获取云账号列表失败: %w", err)
		}
		if len(accts) == 0 {
			return fmt.Errorf("未找到可用的 %s 云账号", params.Provider)
		}
		accounts = accts
		e.logger.Info("查询到活跃云账号",
			elog.String("provider", params.Provider),
			elog.Int("count", len(accounts)))
	}

	totalSynced := 0
	totalAccounts := len(accounts)

	for ai, account := range accounts {
		accountProgress := 20 + (ai*70)/totalAccounts
		e.taskRepo.UpdateProgress(ctx, t.ID, accountProgress,
			fmt.Sprintf("正在同步账号 %s (%d/%d)", account.Name, ai+1, totalAccounts))

		// 创建适配器
		adapter, err := e.adapterFactory.CreateAdapterFromDomain(&account)
		if err != nil {
			e.logger.Error("创建适配器失败",
				elog.String("account", account.Name),
				elog.FieldErr(err))
			continue
		}

		// 获取地域列表
		regions, err := adapter.GetRegions(ctx)
		if err != nil {
			e.logger.Error("获取地域列表失败",
				elog.String("account", account.Name),
				elog.FieldErr(err))
			continue
		}

		// 过滤地域
		if len(params.Regions) > 0 {
			regionMap := make(map[string]bool)
			for _, r := range params.Regions {
				regionMap[r] = true
			}
			filteredRegions := make([]types.Region, 0)
			for _, r := range regions {
				if regionMap[r.ID] {
					filteredRegions = append(filteredRegions, r)
				}
			}
			regions = filteredRegions
		}

		// 同步该账号的所有地域资产
		accountSynced := 0
		totalRegions := len(regions)
		for i, region := range regions {
			regionProgress := accountProgress + (i*70/totalAccounts)/totalRegions
			if regionProgress > 90 {
				regionProgress = 90
			}
			e.taskRepo.UpdateProgress(ctx, t.ID, regionProgress,
				fmt.Sprintf("账号 %s: 正在同步地域 %s (%d/%d)", account.Name, region.ID, i+1, totalRegions))

			synced, err := e.syncRegionAssets(ctx, adapter, &account, region.ID, params.AssetTypes)
			if err != nil {
				e.logger.Error("同步地域资产失败",
					elog.String("account", account.Name),
					elog.String("region", region.ID),
					elog.FieldErr(err))
				continue
			}
			accountSynced += synced
		}

		// DNS 是全局服务，在账号级别同步（不按地域）
		expandedTypes := expandAssetTypes(params.AssetTypes)
		for _, at := range expandedTypes {
			if at == "dns" {
				cloudxAdapter, cloudxErr := e.cloudxFactory.CreateAdapter(&account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败(DNS)", elog.FieldErr(cloudxErr))
					break
				}
				synced, err := e.syncDNS(ctx, cloudxAdapter, &account)
				if err != nil {
					e.logger.Error("同步DNS失败",
						elog.String("account", account.Name),
						elog.FieldErr(err))
				} else {
					accountSynced += synced
				}
				break
			}
		}

		// 更新该账号的最后同步时间
		if err := e.accountRepo.UpdateSyncTime(ctx, account.ID, time.Now(), int64(accountSynced)); err != nil {
			e.logger.Error("更新同步时间失败",
				elog.Int64("account_id", account.ID),
				elog.FieldErr(err))
		}

		totalSynced += accountSynced
	}

	// 更新进度
	e.taskRepo.UpdateProgress(ctx, t.ID, 95, "正在更新同步状态")

	// 构建结果
	result := SyncAssetsResult{
		TotalCount: totalSynced,
		Details: map[string]any{
			"accounts_synced": totalAccounts,
			"asset_types":     params.AssetTypes,
		},
	}

	resultBytes, _ := json.Marshal(result)
	var resultMap map[string]any
	json.Unmarshal(resultBytes, &resultMap)

	t.Result = resultMap
	t.Progress = 100
	t.Message = fmt.Sprintf("同步完成，共同步 %d 个账号 %d 个资产", totalAccounts, totalSynced)

	e.logger.Info("同步资产任务执行完成",
		elog.String("task_id", t.ID),
		elog.Int("total_synced", totalSynced))

	return nil
}

// expandAssetTypes 展开资产类型，支持 database, network, storage, middleware, compute 等聚合类型
func expandAssetTypes(assetTypes []string) []string {
	expanded := make([]string, 0, len(assetTypes)*3)
	seen := make(map[string]bool)

	for _, t := range assetTypes {
		switch t {
		case "database", "db":
			// database 展开为 rds, redis, mongodb
			for _, dbType := range domain.DatabaseAssetTypes {
				if !seen[dbType] {
					expanded = append(expanded, dbType)
					seen[dbType] = true
				}
			}
		case "network", "net":
			// network 展开为 vpc, vswitch, eip, eni, lb, cdn, waf, dns
			for _, netType := range domain.NetworkAssetTypes {
				if !seen[netType] {
					expanded = append(expanded, netType)
					seen[netType] = true
				}
			}
		case "storage":
			// storage 展开为 nas, oss
			for _, storageType := range domain.StorageAssetTypes {
				if !seen[storageType] {
					expanded = append(expanded, storageType)
					seen[storageType] = true
				}
			}
		case "middleware", "mw":
			// middleware 展开为 kafka, elasticsearch
			for _, mwType := range domain.MiddlewareAssetTypes {
				if !seen[mwType] {
					expanded = append(expanded, mwType)
					seen[mwType] = true
				}
			}
		case "compute":
			// compute 展开为 ecs, disk, snapshot, security_group, image
			for _, computeType := range domain.ComputeAssetTypes {
				if !seen[computeType] {
					expanded = append(expanded, computeType)
					seen[computeType] = true
				}
			}
		default:
			if !seen[t] {
				expanded = append(expanded, t)
				seen[t] = true
			}
		}
	}
	return expanded
}

// syncRegionAssets 同步单个地域的资产
func (e *SyncAssetsExecutor) syncRegionAssets(
	ctx context.Context,
	adapter asset.CloudAssetAdapter,
	account *domain.CloudAccount,
	region string,
	assetTypes []string,
) (int, error) {
	totalSynced := 0

	// 展开资产类型（支持 database -> rds, redis, mongodb）
	expandedTypes := expandAssetTypes(assetTypes)

	// 获取 cloudx 适配器用于数据库资源同步
	var cloudxAdapter cloudx.CloudAdapter
	var cloudxErr error

	for _, assetType := range expandedTypes {
		switch assetType {
		case "ecs":
			synced, err := e.syncRegionECS(ctx, adapter, account, region)
			if err != nil {
				e.logger.Error("同步ECS失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "rds":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionRDS(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步RDS失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "redis":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionRedis(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步Redis失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "mongodb":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionMongoDB(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步MongoDB失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "vpc":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionVPC(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步VPC失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "eip":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionEIP(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步EIP失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "eni":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionENI(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步ENI失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "lb":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionLB(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步LB失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "nas":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionNAS(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步NAS失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "oss":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionOSS(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步OSS失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "kafka":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionKafka(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步Kafka失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "elasticsearch", "es":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionElasticsearch(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步Elasticsearch失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "disk":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionDisk(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步云盘失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "snapshot":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionSnapshot(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步快照失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "security_group", "securitygroup", "sg":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionSecurityGroup(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步安全组失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "image":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionImage(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步镜像失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "vswitch", "subnet":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionVSwitch(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步VSwitch失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "cdn":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionCDN(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步CDN失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "waf":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionWAF(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步WAF失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "dns":
			// DNS 是全局服务，在 syncRegionAssets 中跳过
			// DNS 同步在账号级别处理（见 Execute 方法中的 syncDNS 调用）
			continue
		default:
			e.logger.Warn("不支持的资源类型", elog.String("asset_type", assetType))
		}
	}

	return totalSynced, nil
}

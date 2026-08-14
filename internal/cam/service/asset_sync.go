// Package service 资产同步服务（API 触发的同步路径）
//
// 文件：internal/cam/service/asset_sync.go
//
// 作用：提供 AssetSyncService 接口，供 HTTP API（如 /cam/assets/sync）和
//
//	SyncAccountAssets 等服务层方法调用，将云资产同步写入 CMDB c_instance 表。
//	同时负责同步资产关系（ECS→VPC、EIP→ECS 等）。
//
// 与其他同步文件的关系：
//   - internal/cam/task/executor/sync_assets.go  ← 异步任务执行器（定时任务/手动触发任务队列），
//     通过 wire 注入，是生产环境主要的同步入口。
//   - internal/task/executor/sync_assets.go       ← 旧版/备用执行器，不参与运行时。
//   - 本文件（asset_sync.go）                      ← API 直接调用的同步服务。
//
// 注意：两条同步路径写入同一个 c_instance 集合，model_uid 必须保持一致，
//
//	统一使用 fmt.Sprintf("%s_xxx", account.Provider) 格式（如 aliyun_ecs）。
package service

import (
	"context"
	"fmt"
	"time"

	auditdomain "github.com/Havens-blog/e-cam-service/internal/audit/domain"
	auditservice "github.com/Havens-blog/e-cam-service/internal/audit/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/mongo"
)

// AssetSyncService 资产同步服务 - 同步到 CMDB c_instance
type AssetSyncService interface {
	// SyncAssets 同步云资产到 CMDB
	SyncAssets(ctx context.Context, tenantID int64, provider string, assetTypes []string) (*SyncResult, error)
	// SyncAccountAssets 同步指定账号的资产
	SyncAccountAssets(ctx context.Context, tenantID int64, accountID int64, assetTypes []string) (*SyncResult, error)
	// SyncRelations 同步资产关系
	SyncRelations(ctx context.Context, tenantID int64) (*RelationSyncResult, error)
	// SetChangeTracker 设置变更追踪器（可选）
	SetChangeTracker(ct *auditservice.ChangeTracker)
	// SetDNSCollections 设置 DNS 专用集合（可选）
	SetDNSCollections(domainColl, recordColl *mongo.Collection)
}

// SyncResult 同步结果
type SyncResult struct {
	TotalSynced int            `json:"total_synced"`
	Created     int            `json:"created"`
	Updated     int            `json:"updated"`
	Failed      int            `json:"failed"`
	ByAssetType map[string]int `json:"by_asset_type"`
	ByRegion    map[string]int `json:"by_region"`
	StartTime   time.Time      `json:"start_time"`
	EndTime     time.Time      `json:"end_time"`
	DurationMs  int64          `json:"duration_ms"`
}

// RelationSyncResult 关系同步结果
type RelationSyncResult struct {
	TotalSynced    int            `json:"total_synced"`
	Created        int            `json:"created"`
	Skipped        int            `json:"skipped"`
	Failed         int            `json:"failed"`
	ByRelationType map[string]int `json:"by_relation_type"`
	StartTime      time.Time      `json:"start_time"`
	EndTime        time.Time      `json:"end_time"`
	DurationMs     int64          `json:"duration_ms"`
}

type assetSyncService struct {
	instanceRepo   repository.InstanceRepository
	relationRepo   repository.InstanceRelationRepository
	accountRepo    repository.CloudAccountRepository
	adapterFactory *cloudx.AdapterFactory
	changeTracker  *auditservice.ChangeTracker // 资产变更追踪器（可选）
	dnsDomainColl  *mongo.Collection           // DNS 域名集合
	dnsRecordColl  *mongo.Collection           // DNS 记录集合
	logger         *elog.Component
}

// NewAssetSyncService 创建资产同步服务
func NewAssetSyncService(
	instanceRepo repository.InstanceRepository,
	relationRepo repository.InstanceRelationRepository,
	accountRepo repository.CloudAccountRepository,
	adapterFactory *cloudx.AdapterFactory,
	logger *elog.Component,
) AssetSyncService {
	return &assetSyncService{
		instanceRepo:   instanceRepo,
		relationRepo:   relationRepo,
		accountRepo:    accountRepo,
		adapterFactory: adapterFactory,
		logger:         logger,
	}
}

// SetChangeTracker 设置变更追踪器（可选注入，不影响原有构造函数签名）
func (s *assetSyncService) SetChangeTracker(ct *auditservice.ChangeTracker) {
	s.changeTracker = ct
}

// SetDNSCollections 设置 DNS 专用集合（可选注入）
func (s *assetSyncService) SetDNSCollections(domainColl, recordColl *mongo.Collection) {
	s.dnsDomainColl = domainColl
	s.dnsRecordColl = recordColl
}

// trackAndUpsert 在 Upsert 前追踪变更，然后执行 Upsert
func (s *assetSyncService) trackAndUpsert(ctx context.Context, instance domain.Instance) error {
	if s.changeTracker != nil {
		// 查询旧实例
		old, err := s.instanceRepo.GetByAssetID(ctx, instance.TenantID, instance.ModelUID, instance.AssetID)
		if err != nil {
			s.logger.Warn("查询旧实例用于变更追踪失败",
				elog.FieldErr(err),
				elog.String("asset_id", instance.AssetID),
			)
		} else if old.AssetID != "" && old.Attributes != nil {
			// 旧实例存在，追踪变更
			meta := auditdomain.ChangeMetadata{
				AssetID:      instance.AssetID,
				AssetName:    instance.AssetName,
				ModelUID:     instance.ModelUID,
				TenantID:     instance.TenantID,
				AccountID:    instance.AccountID,
				ChangeSource: "sync_task",
			}
			// 从新属性中提取 provider 和 region
			if p, ok := instance.Attributes["provider"].(string); ok {
				meta.Provider = p
			}
			if r, ok := instance.Attributes["region"].(string); ok {
				meta.Region = r
			}
			// TrackChanges 内部失败仅记录日志，不影响同步
			_, _ = s.changeTracker.TrackChanges(ctx, meta, old.Attributes, instance.Attributes)
		}
	}
	return s.instanceRepo.Upsert(ctx, instance)
}

// cleanupStaleInstances 清理云端已不存在的本地实例
func (s *assetSyncService) cleanupStaleInstances(
	ctx context.Context,
	tenantID int64, modelUID string,
	accountID int64,
	region string,
	cloudAssetIDs map[string]bool,
) int {
	var localAssetIDs []string
	var err error
	if region != "" {
		localAssetIDs, err = s.instanceRepo.ListAssetIDsByRegion(ctx, tenantID, modelUID, accountID, region)
	} else {
		localAssetIDs, err = s.instanceRepo.ListAssetIDsByModelUID(ctx, tenantID, modelUID, accountID)
	}
	if err != nil {
		s.logger.Warn("获取本地实例列表失败", elog.FieldErr(err))
		return 0
	}

	var toDelete []string
	for _, assetID := range localAssetIDs {
		if !cloudAssetIDs[assetID] {
			toDelete = append(toDelete, assetID)
		}
	}

	if len(toDelete) > 0 {
		deleted, err := s.instanceRepo.DeleteByAssetIDs(ctx, tenantID, modelUID, toDelete)
		if err != nil {
			s.logger.Error("删除过期实例失败", elog.String("model_uid", modelUID), elog.FieldErr(err))
			return 0
		}
		s.logger.Info("删除过期实例",
			elog.String("model_uid", modelUID),
			elog.String("region", region),
			elog.Int64("deleted", deleted))
		return int(deleted)
	}
	return 0
}

// SyncAssets 同步云资产到 CMDB
func (s *assetSyncService) SyncAssets(ctx context.Context, tenantID int64, provider string, assetTypes []string) (*SyncResult, error) {
	if tenantID == 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}

	if len(assetTypes) == 0 {
		assetTypes = []string{
			"ecs", "disk", "snapshot", "security_group", "image",
			"rds", "redis", "mongodb",
			"vpc", "eip", "lb", "cdn", "waf", "eni", "dns",
			"nas", "oss",
		}
	}

	result := &SyncResult{
		ByAssetType: make(map[string]int),
		ByRegion:    make(map[string]int),
		StartTime:   time.Now(),
	}

	s.logger.Info("开始同步云资产到CMDB",
		elog.Int64("tenant_id", tenantID),
		elog.String("provider", provider),
		elog.Any("asset_types", assetTypes))

	// 获取该云厂商的所有可用账号
	filter := shareddomain.CloudAccountFilter{
		Provider: shareddomain.CloudProvider(provider),
		Status:   shareddomain.CloudAccountStatusActive,
		Limit:    100,
	}

	accounts, _, err := s.accountRepo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("获取云账号失败: %w", err)
	}

	if len(accounts) == 0 {
		return nil, fmt.Errorf("未找到可用的 %s 云账号", provider)
	}

	// 同步每个账号
	for i := range accounts {
		accountResult, err := s.syncSingleAccount(ctx, tenantID, &accounts[i], assetTypes)
		if err != nil {
			s.logger.Error("同步账号资产失败",
				elog.String("account", accounts[i].Name),
				elog.FieldErr(err))
			continue
		}
		s.mergeResult(result, accountResult)
	}

	result.EndTime = time.Now()
	result.DurationMs = result.EndTime.Sub(result.StartTime).Milliseconds()

	s.logger.Info("云资产同步完成",
		elog.String("provider", provider),
		elog.Int("total_synced", result.TotalSynced),
		elog.Int64("duration_ms", result.DurationMs))

	return result, nil
}

// SyncAccountAssets 同步指定账号的资产
func (s *assetSyncService) SyncAccountAssets(ctx context.Context, tenantID int64, accountID int64, assetTypes []string) (*SyncResult, error) {
	if tenantID == 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}

	if len(assetTypes) == 0 {
		assetTypes = []string{
			"ecs", "disk", "snapshot", "security_group", "image",
			"rds", "redis", "mongodb",
			"vpc", "eip", "lb", "cdn", "waf", "eni", "dns",
			"nas", "oss",
		}
	}

	// 获取账号信息
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("获取云账号失败: %w", err)
	}

	result := &SyncResult{
		ByAssetType: make(map[string]int),
		ByRegion:    make(map[string]int),
		StartTime:   time.Now(),
	}

	accountResult, err := s.syncSingleAccount(ctx, tenantID, &account, assetTypes)
	if err != nil {
		return nil, err
	}
	s.mergeResult(result, accountResult)

	result.EndTime = time.Now()
	result.DurationMs = result.EndTime.Sub(result.StartTime).Milliseconds()

	return result, nil
}

// syncSingleAccount 同步单个账号的资产
func (s *assetSyncService) syncSingleAccount(
	ctx context.Context,
	tenantID int64,
	account *shareddomain.CloudAccount,
	assetTypes []string,
) (*SyncResult, error) {
	result := &SyncResult{
		ByAssetType: make(map[string]int),
		ByRegion:    make(map[string]int),
	}

	// 使用 cloudx 适配器工厂创建适配器
	adapter, err := s.adapterFactory.CreateAdapter(account)
	if err != nil {
		return nil, fmt.Errorf("创建适配器失败: %w", err)
	}

	// 获取所有地域
	regions, err := adapter.ECS().GetRegions(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取地域列表失败: %w", err)
	}

	// 过滤支持的地域
	if len(account.Config.SupportedRegions) > 0 {
		regionMap := make(map[string]bool)
		for _, r := range account.Config.SupportedRegions {
			regionMap[r] = true
		}
		filtered := make([]types.Region, 0)
		for _, r := range regions {
			if regionMap[r.ID] {
				filtered = append(filtered, r)
			}
		}
		regions = filtered
	}

	s.logger.Debug("开始同步地域资产",
		elog.String("account", account.Name),
		elog.Int("region_count", len(regions)))

	// 同步每个地域
	for _, region := range regions {
		regionResult, err := s.syncRegion(ctx, tenantID, adapter, account, region.ID, assetTypes)
		if err != nil {
			s.logger.Error("同步地域资产失败",
				elog.String("region", region.ID),
				elog.FieldErr(err))
			continue
		}
		s.mergeResult(result, regionResult)
	}

	// 更新账号同步时间
	now := time.Now()
	_ = s.accountRepo.UpdateSyncTime(ctx, account.ID, now, int64(result.TotalSynced))

	return result, nil
}

// syncRegion 同步单个地域的资产
func (s *assetSyncService) syncRegion(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
	assetTypes []string,
) (*SyncResult, error) {
	result := &SyncResult{
		ByAssetType: make(map[string]int),
		ByRegion:    make(map[string]int),
	}

	for _, assetType := range assetTypes {
		var synced *SyncResult
		var err error

		switch assetType {
		case "ecs", "cloud_vm":
			synced, err = s.syncECSInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步ECS实例失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["ecs"] += synced.TotalSynced

		case "rds", "cloud_rds":
			synced, err = s.syncRDSInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步RDS实例失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["rds"] += synced.TotalSynced

		case "redis", "cloud_redis":
			synced, err = s.syncRedisInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步Redis实例失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["redis"] += synced.TotalSynced

		case "mongodb", "cloud_mongodb":
			synced, err = s.syncMongoDBInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步MongoDB实例失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["mongodb"] += synced.TotalSynced

		case "vpc", "cloud_vpc":
			synced, err = s.syncVPCInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步VPC失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["vpc"] += synced.TotalSynced

		case "eip", "cloud_eip":
			synced, err = s.syncEIPInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步EIP失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["eip"] += synced.TotalSynced

		case "vswitch", "cloud_vswitch", "subnet":
			synced, err = s.syncVSwitchInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步VSwitch失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["vswitch"] += synced.TotalSynced

		case "lb", "cloud_lb", "slb", "alb", "nlb":
			synced, err = s.syncLBInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步LB失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["lb"] += synced.TotalSynced

		case "cdn", "cloud_cdn":
			synced, err = s.syncCDNInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步CDN失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["cdn"] += synced.TotalSynced

		case "waf", "cloud_waf":
			synced, err = s.syncWAFInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步WAF失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["waf"] += synced.TotalSynced

		case "eni", "cloud_eni":
			synced, err = s.syncENIInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步ENI失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["eni"] += synced.TotalSynced

		case "nas", "cloud_nas":
			synced, err = s.syncNASInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步NAS失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["nas"] += synced.TotalSynced

		case "oss", "cloud_oss":
			synced, err = s.syncOSSBuckets(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步OSS失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["oss"] += synced.TotalSynced

		case "disk", "cloud_disk":
			synced, err = s.syncDiskInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步云盘失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["disk"] += synced.TotalSynced

		case "snapshot", "cloud_snapshot":
			synced, err = s.syncSnapshotInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步快照失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["snapshot"] += synced.TotalSynced

		case "security_group", "cloud_security_group":
			synced, err = s.syncSecurityGroupInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步安全组失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["security_group"] += synced.TotalSynced

		case "image", "cloud_image":
			synced, err = s.syncImageInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步镜像失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["image"] += synced.TotalSynced

		case "database":
			// 聚合类型：同步所有数据库资源
			for _, dbType := range []string{"rds", "redis", "mongodb"} {
				dbResult, _ := s.syncRegion(ctx, tenantID, adapter, account, region, []string{dbType})
				if dbResult != nil {
					s.mergeResult(result, dbResult)
				}
			}
			continue

		case "dns", "cloud_dns":
			// DNS 是全局服务，先同步域名再同步记录
			synced, err = s.syncDNSDomains(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步DNS域名失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["dns_domain"] += synced.TotalSynced

			recordsSynced, recordsErr := s.syncDNSRecords(ctx, tenantID, adapter, account, region)
			if recordsErr != nil {
				s.logger.Error("同步DNS记录失败", elog.String("region", region), elog.FieldErr(recordsErr))
			} else if recordsSynced != nil {
				result.ByAssetType["dns_record"] += recordsSynced.TotalSynced
				synced.TotalSynced += recordsSynced.TotalSynced
				synced.Failed += recordsSynced.Failed
			}

		case "network":
			// 聚合类型：同步所有网络资源
			for _, netType := range []string{"vpc", "eip", "lb", "cdn", "waf", "eni", "dns"} {
				netResult, _ := s.syncRegion(ctx, tenantID, adapter, account, region, []string{netType})
				if netResult != nil {
					s.mergeResult(result, netResult)
				}
			}
			continue

		case "storage":
			// 聚合类型：同步所有存储资源
			for _, storageType := range []string{"nas", "oss"} {
				storageResult, _ := s.syncRegion(ctx, tenantID, adapter, account, region, []string{storageType})
				if storageResult != nil {
					s.mergeResult(result, storageResult)
				}
			}
			continue

		default:
			s.logger.Warn("不支持的资源类型", elog.String("asset_type", assetType))
			continue
		}

		if synced != nil {
			result.ByRegion[region] += synced.TotalSynced
			result.TotalSynced += synced.TotalSynced
			result.Created += synced.Created
			result.Updated += synced.Updated
		}
	}

	return result, nil
}

// mergeResult 合并同步结果
func (s *assetSyncService) mergeResult(target, source *SyncResult) {
	target.TotalSynced += source.TotalSynced
	target.Created += source.Created
	target.Updated += source.Updated
	target.Failed += source.Failed

	for k, v := range source.ByAssetType {
		target.ByAssetType[k] += v
	}
	for k, v := range source.ByRegion {
		target.ByRegion[k] += v
	}
}

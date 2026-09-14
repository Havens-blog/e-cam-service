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
//   - internal/cam/service/asset_sync.go          ← 已删除（同步收敛 Phase 2，死服务零调用者）。
//   - internal/task/executor/sync_assets.go       ← 旧版/备用执行器，不参与运行时。
//   - 本文件（sync_assets.go）                     ← 生产环境实际运行的任务执行器。
//
// 注意：本执行器是唯一写入 c_instance 的同步实现，model_uid 必须保持一致，
//
//	统一使用 fmt.Sprintf("%s_xxx", account.Provider) 格式（如 aliyun_ecs）。
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	auditdomain "github.com/Havens-blog/e-cam-service/internal/audit/domain"
	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/asset"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/mongo"
	"strings"
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
	changeTracker  ChangeTracker     // 资产同步变更追踪（nil 不追踪，由 ioc 注入）
	logger         *elog.Component
	// syncingNow 账号级同步互斥(account_id -> task_id)。
	// 手动连点/调度器/重试会产生同一账号的多个并发同步任务,
	// 全量同步互相踩踏浪费厂商 API 配额且更慢,故同账号同时只放行一个任务。
	syncMu     sync.Mutex
	syncingNow map[int64]string
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
		syncingNow:     make(map[int64]string),
	}
}

// tryAcquireAccount 占用账号同步权;已被其他任务持有返回 false(持有者自身幂等)。
func (e *SyncAssetsExecutor) tryAcquireAccount(accountID int64, taskID string) bool {
	e.syncMu.Lock()
	defer e.syncMu.Unlock()
	if owner, busy := e.syncingNow[accountID]; busy && owner != taskID {
		return false
	}
	e.syncingNow[accountID] = taskID
	return true
}

// releaseAccount 释放账号同步权(仅持有者可释放)。
func (e *SyncAssetsExecutor) releaseAccount(accountID int64, taskID string) {
	e.syncMu.Lock()
	defer e.syncMu.Unlock()
	if owner, busy := e.syncingNow[accountID]; busy && owner == taskID {
		delete(e.syncingNow, accountID)
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
	skippedAccounts := make([]string, 0)

	for ai, account := range accounts {
		// 账号级互斥:该账号已有同步任务在执行时直接跳过,不与其踩踏
		if !e.tryAcquireAccount(account.ID, t.ID) {
			e.logger.Warn("该账号已有同步任务在执行,本任务跳过该账号",
				elog.String("account", account.Name),
				elog.Int64("account_id", account.ID),
				elog.String("task_id", t.ID))
			skippedAccounts = append(skippedAccounts, account.Name)
			continue
		}

		accountProgress := 20 + (ai*70)/totalAccounts
		e.taskRepo.UpdateProgress(ctx, t.ID, accountProgress,
			fmt.Sprintf("正在同步账号 %s (%d/%d)", account.Name, ai+1, totalAccounts))

		// 创建适配器
		adapter, err := e.adapterFactory.CreateAdapterFromDomain(&account)
		if err != nil {
			e.logger.Error("创建适配器失败",
				elog.String("account", account.Name),
				elog.FieldErr(err))
			e.releaseAccount(account.ID, t.ID)
			continue
		}

		// 获取地域列表
		regions, err := adapter.GetRegions(ctx)
		if err != nil {
			e.logger.Error("获取地域列表失败",
				elog.String("account", account.Name),
				elog.FieldErr(err))
			e.releaseAccount(account.ID, t.ID)
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

		e.releaseAccount(account.ID, t.ID)
		totalSynced += accountSynced
	}

	// 更新进度
	e.taskRepo.UpdateProgress(ctx, t.ID, 95, "正在更新同步状态")

	// 构建结果
	result := SyncAssetsResult{
		TotalCount: totalSynced,
		Details: map[string]any{
			"accounts_synced":  totalAccounts - len(skippedAccounts),
			"accounts_skipped": skippedAccounts,
			"asset_types":      params.AssetTypes,
		},
	}

	resultBytes, _ := json.Marshal(result)
	var resultMap map[string]any
	json.Unmarshal(resultBytes, &resultMap)

	t.Result = resultMap
	t.Progress = 100
	if len(skippedAccounts) > 0 {
		t.Message = fmt.Sprintf("同步完成，共同步 %d 个账号 %d 个资产（%d 个账号因并发同步被跳过: %s）",
			totalAccounts-len(skippedAccounts), totalSynced, len(skippedAccounts), strings.Join(skippedAccounts, "、"))
	} else {
		t.Message = fmt.Sprintf("同步完成，共同步 %d 个账号 %d 个资产", totalAccounts, totalSynced)
	}

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
// ChangeTracker 资产同步变更追踪接口（同步收敛 Phase 2 S3a，替代已删除的
// asset_sync 死服务中的 trackAndUpsert）。nil 时同步不追踪，默认关闭。
type ChangeTracker interface {
	TrackChanges(ctx context.Context, meta auditdomain.ChangeMetadata, oldAttrs, newAttrs map[string]interface{}) (int, error)
}

// SetChangeTracker 注入资产同步变更追踪（nil 关闭追踪）
func (e *SyncAssetsExecutor) SetChangeTracker(t ChangeTracker) {
	e.changeTracker = t
}

// syncItem 待同步的一条云资产：AssetID 用于差集删除，ToInstance 负责转换+upsert 由调用方闭包提供
type syncItem struct {
	AssetID    string
	ToInstance func() (camdomain.Instance, error)
}

// diffAndUpsert 通用"对比本地 → 删除过期 → 新增/更新"（同步收敛 Phase 2 S5）。
// 各 syncRegion<X> 构建 []syncItem 后调用本方法，消除 19 份逐文件复制。
// 语义与既有实现一致：ListAssetIDsByRegion 失败按空本地处理；删除失败仅记日志不阻断；
// 单条转换/upsert 失败跳过不计入 synced。
func (e *SyncAssetsExecutor) diffAndUpsert(
	ctx context.Context,
	tenantID int64,
	modelUID string,
	accountID int64,
	region string,
	items []syncItem,
) (synced int, deleted int64, err error) {
	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, tenantID, modelUID, accountID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	cloudSet := make(map[string]bool, len(items))
	for _, it := range items {
		cloudSet[it.AssetID] = true
	}

	var toDelete []string
	for _, assetID := range localAssetIDs {
		if !cloudSet[assetID] {
			toDelete = append(toDelete, assetID)
		}
	}

	if len(toDelete) > 0 {
		d, delErr := e.instanceRepo.DeleteByAssetIDs(ctx, tenantID, modelUID, toDelete)
		if delErr != nil {
			e.logger.Error("删除过期实例失败", elog.FieldErr(delErr))
		} else {
			deleted = d
		}
	}

	for _, it := range items {
		instance, convErr := it.ToInstance()
		if convErr != nil {
			e.logger.Error("转换实例失败", elog.String("asset_id", it.AssetID), elog.FieldErr(convErr))
			continue
		}
		// 变更追踪（可选注入）：有旧实例才记录，失败不影响同步
		if e.changeTracker != nil {
			e.trackChange(ctx, instance)
		}
		if upErr := e.instanceRepo.Upsert(ctx, instance); upErr != nil {
			e.logger.Error("保存实例失败", elog.String("asset_id", it.AssetID), elog.FieldErr(upErr))
			continue
		}
		synced++
	}

	return synced, deleted, nil
}

// trackChange 查询旧实例并记录变更（同步来源，语义与已删除的 asset_sync trackAndUpsert 一致）
func (e *SyncAssetsExecutor) trackChange(ctx context.Context, instance camdomain.Instance) {
	old, err := e.instanceRepo.GetByAssetID(ctx, instance.TenantID, instance.ModelUID, instance.AssetID)
	if err != nil {
		e.logger.Warn("查询旧实例用于变更追踪失败",
			elog.FieldErr(err),
			elog.String("asset_id", instance.AssetID))
		return
	}
	if old.AssetID == "" || old.Attributes == nil {
		return
	}

	meta := auditdomain.ChangeMetadata{
		AssetID:      instance.AssetID,
		AssetName:    instance.AssetName,
		ModelUID:     instance.ModelUID,
		TenantID:     instance.TenantID,
		AccountID:    instance.AccountID,
		ChangeSource: "sync_task",
	}
	if p, ok := instance.Attributes["provider"].(string); ok {
		meta.Provider = p
	}
	if r, ok := instance.Attributes["region"].(string); ok {
		meta.Region = r
	}
	// TrackChanges 内部失败仅记录日志，不影响同步
	_, _ = e.changeTracker.TrackChanges(ctx, meta, old.Attributes, instance.Attributes)
}

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
		if assetType == dnsAssetType {
			// DNS 是全局服务，在 syncRegionAssets 中跳过（账号级处理，见 Execute 中的 syncDNS）
			continue
		}

		// 计算型资源（ECS）使用 asset 适配器，无需 cloudx
		if entry, ok := assetSyncFns[assetType]; ok {
			synced, err := entry.fn(e, ctx, adapter, account, region)
			if err != nil {
				e.logger.Error(entry.label, elog.String("region", region), elog.FieldErr(err))
				continue
			}
			totalSynced += synced
			continue
		}

		entry, ok := cloudxSyncFns[assetType]
		if !ok {
			// 未注册类型不触碰工厂，直接告警（与旧 switch default 行为一致）
			e.logger.Warn("不支持的资源类型", elog.String("asset_type", assetType))
			continue
		}

		// 其余资源懒加载 cloudx 适配器（创建失败/不可用时跳过本类型）
		if cloudxAdapter == nil && cloudxErr == nil {
			cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
			if cloudxErr != nil {
				e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
			}
		}
		if cloudxAdapter == nil {
			continue
		}
		synced, err := entry.fn(e, ctx, cloudxAdapter, account, region)
		if err != nil {
			e.logger.Error(entry.label, elog.String("region", region), elog.FieldErr(err))
			continue
		}
		totalSynced += synced
	}

	return totalSynced, nil
}

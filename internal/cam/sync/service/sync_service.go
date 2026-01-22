package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/sync/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/sync/service/adapters"
	"github.com/gotomicro/ego/core/elog"
)

// SyncService 同步服务
type SyncService struct {
	adapterFactory *adapters.AdapterFactory
	logger         *elog.Component
	// TODO: 添加 repository 依赖
}

// NewSyncService 创建同步服务
func NewSyncService(
	adapterFactory *adapters.AdapterFactory,
	logger *elog.Component,
) *SyncService {
	return &SyncService{
		adapterFactory: adapterFactory,
		logger:         logger,
	}
}

// SyncECSInstances 同步云主机实例
func (s *SyncService) SyncECSInstances(ctx context.Context, account *domain.CloudAccount, regions []string) (*domain.SyncResult, error) {
	s.logger.Info("开始同步云主机实例",
		elog.String("account", account.Name),
		elog.String("provider", string(account.Provider)),
		elog.Any("regions", regions))

	startTime := time.Now()

	// 创建适配器
	adapter, err := s.adapterFactory.CreateAdapter(account)
	if err != nil {
		return nil, fmt.Errorf("创建适配器失败: %w", err)
	}

	// 如果没有指定地域，获取所有地域
	if len(regions) == 0 {
		allRegions, err := adapter.GetRegions(ctx)
		if err != nil {
			return nil, fmt.Errorf("获取地域列表失败: %w", err)
		}
		regions = make([]string, 0, len(allRegions))
		for _, r := range allRegions {
			regions = append(regions, r.ID)
		}
	}

	// 并发同步多个地域
	result := &domain.SyncResult{
		Success: true,
		Errors:  make([]domain.SyncError, 0),
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5) // 限制并发数为5

	for _, region := range regions {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			regionResult, err := s.syncRegionECSInstances(ctx, adapter, account, r)
			if err != nil {
				s.logger.Error("同步地域失败",
					elog.String("region", r),
					elog.FieldErr(err))

				mu.Lock()
				result.Success = false
				result.ErrorCount++
				result.Errors = append(result.Errors, domain.SyncError{
					ResourceID: r,
					Error:      err.Error(),
					Timestamp:  time.Now(),
				})
				mu.Unlock()
				return
			}

			// 合并结果
			mu.Lock()
			result.TotalCount += regionResult.TotalCount
			result.AddedCount += regionResult.AddedCount
			result.UpdatedCount += regionResult.UpdatedCount
			result.DeletedCount += regionResult.DeletedCount
			result.UnchangedCount += regionResult.UnchangedCount
			result.ErrorCount += regionResult.ErrorCount
			result.Errors = append(result.Errors, regionResult.Errors...)
			mu.Unlock()
		}(region)
	}

	wg.Wait()
	result.Duration = time.Since(startTime)

	s.logger.Info("云主机实例同步完成",
		elog.String("account", account.Name),
		elog.Int("total", result.TotalCount),
		elog.Int("added", result.AddedCount),
		elog.Int("updated", result.UpdatedCount),
		elog.Int("deleted", result.DeletedCount),
		elog.Int("unchanged", result.UnchangedCount),
		elog.Int("errors", result.ErrorCount),
		elog.Duration("duration", result.Duration))

	return result, nil
}

// syncRegionECSInstances 同步单个地域的云主机实例
func (s *SyncService) syncRegionECSInstances(
	ctx context.Context,
	adapter domain.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (*domain.SyncResult, error) {
	s.logger.Info("同步地域云主机实例",
		elog.String("region", region),
		elog.String("provider", string(account.Provider)))

	result := &domain.SyncResult{
		Success: true,
		Errors:  make([]domain.SyncError, 0),
	}

	// 从云厂商获取实例列表
	instances, err := adapter.GetECSInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取实例列表失败: %w", err)
	}

	result.TotalCount = len(instances)

	// TODO: 实现实际的数据库操作
	// 1. 获取数据库中已存在的实例
	// 2. 比对差异，确定新增、更新、删除的实例
	// 3. 执行数据库操作

	// 临时实现：假设所有实例都是新增的
	for _, inst := range instances {
		s.logger.Debug("处理实例",
			elog.String("instance_id", inst.InstanceID),
			elog.String("instance_name", inst.InstanceName),
			elog.String("status", inst.Status))

		// TODO: 保存到数据库
		result.AddedCount++
	}

	s.logger.Info("地域云主机实例同步完成",
		elog.String("region", region),
		elog.Int("total", result.TotalCount),
		elog.Int("added", result.AddedCount))

	return result, nil
}

// DetectInstanceChanges 检测实例变化
func (s *SyncService) DetectInstanceChanges(
	existingInstances map[string]*domain.ECSInstance,
	newInstances []domain.ECSInstance,
) (added, updated, deleted, unchanged []domain.ECSInstance) {
	// 创建新实例的映射
	newInstanceMap := make(map[string]*domain.ECSInstance)
	for i := range newInstances {
		newInstanceMap[newInstances[i].InstanceID] = &newInstances[i]
	}

	// 检测新增和更新的实例
	for _, newInst := range newInstances {
		existingInst, exists := existingInstances[newInst.InstanceID]
		if !exists {
			// 新增实例
			added = append(added, newInst)
		} else if s.isInstanceChanged(existingInst, &newInst) {
			// 实例有变化
			updated = append(updated, newInst)
		} else {
			// 实例无变化
			unchanged = append(unchanged, newInst)
		}
	}

	// 检测已删除的实例
	for instanceID, existingInst := range existingInstances {
		if _, exists := newInstanceMap[instanceID]; !exists {
			deleted = append(deleted, *existingInst)
		}
	}

	return
}

// isInstanceChanged 判断实例是否有变化
func (s *SyncService) isInstanceChanged(old, new *domain.ECSInstance) bool {
	// 比较关键字段
	if old.Status != new.Status {
		return true
	}
	if old.InstanceName != new.InstanceName {
		return true
	}
	if old.PublicIP != new.PublicIP {
		return true
	}
	if old.PrivateIP != new.PrivateIP {
		return true
	}
	if old.InstanceType != new.InstanceType {
		return true
	}
	if len(old.SecurityGroups) != len(new.SecurityGroups) {
		return true
	}
	if len(old.DataDisks) != len(new.DataDisks) {
		return true
	}

	// 比较标签
	if len(old.Tags) != len(new.Tags) {
		return true
	}
	for k, v := range old.Tags {
		if newV, ok := new.Tags[k]; !ok || newV != v {
			return true
		}
	}

	return false
}

// SyncECSInstancesIncremental 增量同步云主机实例
func (s *SyncService) SyncECSInstancesIncremental(
	ctx context.Context,
	account *domain.CloudAccount,
	regions []string,
	lastSyncTime time.Time,
) (*domain.SyncResult, error) {
	s.logger.Info("开始增量同步云主机实例",
		elog.String("account", account.Name),
		elog.String("last_sync_time", lastSyncTime.Format("2006-01-02 15:04:05")))

	// 增量同步的逻辑：
	// 1. 获取最新的实例列表
	// 2. 与数据库中的实例比对
	// 3. 只更新有变化的实例

	result, err := s.SyncECSInstances(ctx, account, regions)
	if err != nil {
		return nil, err
	}

	// TODO: 实现真正的增量同步逻辑
	// 1. 查询数据库中 last_sync_time 之后更新的实例
	// 2. 只同步这些实例的最新状态
	// 3. 检测已删除的实例

	return result, nil
}

// GetInstanceStatusChanges 获取实例状态变化
func (s *SyncService) GetInstanceStatusChanges(
	ctx context.Context,
	account *domain.CloudAccount,
	region string,
	instanceIDs []string,
) (map[string]string, error) {
	s.logger.Info("获取实例状态变化",
		elog.String("region", region),
		elog.Int("instance_count", len(instanceIDs)))

	adapter, err := s.adapterFactory.CreateAdapter(account)
	if err != nil {
		return nil, fmt.Errorf("创建适配器失败: %w", err)
	}

	// 获取实例列表
	instances, err := adapter.GetECSInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取实例列表失败: %w", err)
	}

	// 构建实例ID到状态的映射
	statusMap := make(map[string]string)
	for _, inst := range instances {
		for _, id := range instanceIDs {
			if inst.InstanceID == id {
				statusMap[id] = inst.Status
				break
			}
		}
	}

	return statusMap, nil
}

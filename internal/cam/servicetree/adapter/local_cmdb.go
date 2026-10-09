// Package adapter 提供 CMDBPort 的本地实现（进程内直接调用 cmdb repository）。
// 当 CMDB 拆分为独立服务后，可替换为 HTTP/gRPC 客户端实现。
package adapter

import (
	"context"

	cmdbdomain "github.com/Havens-blog/e-cam-service/internal/cmdb/domain"
	cmdbrepository "github.com/Havens-blog/e-cam-service/internal/cmdb/repository"
	cmdbdao "github.com/Havens-blog/e-cam-service/internal/cmdb/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/port"
)

// LocalCMDBAdapter 本地 CMDB 适配器（进程内调用）
type LocalCMDBAdapter struct {
	repo cmdbrepository.InstanceRepository
}

// NewLocalCMDBAdapter 创建本地 CMDB 适配器
func NewLocalCMDBAdapter(repo cmdbrepository.InstanceRepository) *LocalCMDBAdapter {
	return &LocalCMDBAdapter{repo: repo}
}

func (a *LocalCMDBAdapter) ListByIDs(ctx context.Context, ids []int64) ([]port.CMDBInstance, error) {
	instances, err := a.repo.ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	return convertInstances(instances), nil
}

func (a *LocalCMDBAdapter) ListUnbound(ctx context.Context, tenantID int64, offset, limit int64) ([]port.CMDBInstance, error) {
	instances, err := a.repo.ListUnbound(ctx, tenantID, offset, limit)
	if err != nil {
		return nil, err
	}
	return convertInstances(instances), nil
}

func (a *LocalCMDBAdapter) CountUnbound(ctx context.Context, tenantID int64) (int64, error) {
	return a.repo.CountUnbound(ctx, tenantID)
}

func (a *LocalCMDBAdapter) AggregateStatsByIDs(ctx context.Context, ids []int64) (*port.AssetStatsResult, error) {
	result, err := a.repo.AggregateStatsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	return convertStatsResult(result), nil
}

func (a *LocalCMDBAdapter) AggregateAllStats(ctx context.Context, tenantID int64) (*port.AssetStatsResult, error) {
	result, err := a.repo.AggregateAllStats(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return convertStatsResult(result), nil
}

func (a *LocalCMDBAdapter) AggregateUnboundStats(ctx context.Context, tenantID int64) (*port.AssetStatsResult, error) {
	result, err := a.repo.AggregateUnboundStats(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return convertStatsResult(result), nil
}

// convertInstances 将 cmdb domain.Instance 转换为 port.CMDBInstance
func convertInstances(instances []cmdbdomain.Instance) []port.CMDBInstance {
	result := make([]port.CMDBInstance, len(instances))
	for i, inst := range instances {
		result[i] = port.CMDBInstance{
			ID:         inst.ID,
			ModelUID:   inst.ModelUID,
			AssetID:    inst.AssetID,
			AssetName:  inst.AssetName,
			TenantID:   inst.TenantID,
			AccountID:  inst.AccountID,
			Attributes: inst.Attributes,
			CreateTime: inst.CreateTime,
			UpdateTime: inst.UpdateTime,
		}
	}
	return result
}

// convertStatsResult 将 cmdb dao.AssetStatsResult 转换为 port.AssetStatsResult
func convertStatsResult(result *cmdbdao.AssetStatsResult) *port.AssetStatsResult {
	if result == nil {
		return &port.AssetStatsResult{
			ByAssetType: []port.AssetTypeCount{},
			ByProvider:  []port.ProviderCount{},
		}
	}

	byAssetType := make([]port.AssetTypeCount, len(result.ByAssetType))
	for i, item := range result.ByAssetType {
		byAssetType[i] = port.AssetTypeCount{
			AssetType: item.AssetType,
			Count:     item.Count,
		}
	}

	byProvider := make([]port.ProviderCount, len(result.ByProvider))
	for i, item := range result.ByProvider {
		byProvider[i] = port.ProviderCount{
			Provider: item.Provider,
			Count:    item.Count,
		}
	}

	return &port.AssetStatsResult{
		Total:       result.Total,
		ByAssetType: byAssetType,
		ByProvider:  byProvider,
	}
}

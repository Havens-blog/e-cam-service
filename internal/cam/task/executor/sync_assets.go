package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gotomicro/ego/core/elog"
)

// 定义任务类型常量，避免循环导入
const (
	TaskTypeSyncAssets taskx.TaskType = "cam:sync_assets"
)

// SyncAssetsExecutor 同步资产任务执行器
type SyncAssetsExecutor struct {
	assetService service.Service
	taskRepo     taskx.TaskRepository
	logger       *elog.Component
}

// NewSyncAssetsExecutor 创建同步资产任务执行器
func NewSyncAssetsExecutor(
	assetService service.Service,
	taskRepo taskx.TaskRepository,
	logger *elog.Component,
) *SyncAssetsExecutor {
	return &SyncAssetsExecutor{
		assetService: assetService,
		taskRepo:     taskRepo,
		logger:       logger,
	}
}

// GetType 获取任务类型
func (e *SyncAssetsExecutor) GetType() taskx.TaskType {
	return TaskTypeSyncAssets
}

// Execute 执行任务
func (e *SyncAssetsExecutor) Execute(ctx context.Context, t *taskx.Task) error {
	e.logger.Info("开始执行同步资产任务",
		elog.String("task_id", t.ID))

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

	// 更新进度: 开始同步
	e.taskRepo.UpdateProgress(ctx, t.ID, 10, "开始同步资产")

	// 执行同步
	synced, err := e.assetService.SyncAssets(ctx, params.AccountID, params.AssetTypes)
	if err != nil {
		return fmt.Errorf("同步资产失败: %w", err)
	}

	// 更新进度: 同步完成
	e.taskRepo.UpdateProgress(ctx, t.ID, 90, fmt.Sprintf("资产同步完成，同步 %d 个资产", synced))

	// 获取统计信息
	stats, err := e.assetService.GetAssetStatistics(ctx)
	if err != nil {
		e.logger.Warn("获取统计信息失败", elog.FieldErr(err))
	}

	// 构建任务结果
	result := SyncAssetsResult{
		TotalCount: int(stats.TotalAssets),
		Details: map[string]interface{}{
			"provider_stats":   stats.ProviderStats,
			"asset_type_stats": stats.AssetTypeStats,
			"region_stats":     stats.RegionStats,
		},
	}

	// 将结果转换为 map
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("序列化任务结果失败: %w", err)
	}

	var resultMap map[string]interface{}
	if err := json.Unmarshal(resultBytes, &resultMap); err != nil {
		return fmt.Errorf("转换任务结果失败: %w", err)
	}

	t.Result = resultMap
	t.Progress = 100
	t.Message = "任务执行完成"

	e.logger.Info("同步资产任务执行完成",
		elog.String("task_id", t.ID),
		elog.Int("total_count", result.TotalCount))

	return nil
}

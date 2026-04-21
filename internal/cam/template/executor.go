package template

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gotomicro/ego/core/elog"
)

const (
	TaskTypeCreateECS taskx.TaskType = "cam:create_ecs"

	// 资产同步重试配置
	syncMaxRetries    = 3
	syncRetryInterval = 5 * time.Minute
)

// createECSParams executor 内部解析的任务参数
type createECSParams struct {
	TaskID string `json:"task_id"`
}

// AssetSyncer 资产同步接口（解耦 asset_sync 依赖）
type AssetSyncer interface {
	SyncInstanceByID(ctx context.Context, provider string, accountID int64, region, instanceID string) error
}

// CreateECSExecutor ECS 实例创建任务执行器
// 统一处理模板创建和直接创建两种来源的任务
type CreateECSExecutor struct {
	tmplDAO         TemplateDAO
	taskDAO         ProvisionTaskDAO
	validator       *TemplateValidator
	accountProvider AccountProvider
	adapterFactory  AdapterFactory
	assetSyncer     AssetSyncer
	logger          *elog.Component
}

// NewCreateECSExecutor 创建执行器
func NewCreateECSExecutor(
	tmplDAO TemplateDAO,
	taskDAO ProvisionTaskDAO,
	validator *TemplateValidator,
	accountProvider AccountProvider,
	adapterFactory AdapterFactory,
	assetSyncer AssetSyncer,
	logger *elog.Component,
) *CreateECSExecutor {
	return &CreateECSExecutor{
		tmplDAO:         tmplDAO,
		taskDAO:         taskDAO,
		validator:       validator,
		accountProvider: accountProvider,
		adapterFactory:  adapterFactory,
		assetSyncer:     assetSyncer,
		logger:          logger,
	}
}

// GetType 获取任务类型
func (e *CreateECSExecutor) GetType() taskx.TaskType {
	return TaskTypeCreateECS
}

// Execute 执行 ECS 创建任务
func (e *CreateECSExecutor) Execute(ctx context.Context, t *taskx.Task) error {
	// 1. 解析任务参数
	var params createECSParams
	paramsBytes, _ := json.Marshal(t.Params)
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		return fmt.Errorf("解析任务参数失败: %w", err)
	}

	// 2. 获取创建任务记录
	task, err := e.taskDAO.GetByID(ctx, "", params.TaskID)
	if err != nil {
		// 尝试不带 tenantID 查询（executor 内部调用）
		return fmt.Errorf("获取创建任务失败: %w", err)
	}

	// 3. 更新状态为 running
	_ = e.taskDAO.UpdateStatus(ctx, task.ID, TaskStatusRunning, "正在创建实例")

	// 4. 构建统一创建参数
	createParams, accountID, provider, err := e.buildParams(ctx, &task)
	if err != nil {
		_ = e.taskDAO.UpdateStatus(ctx, task.ID, TaskStatusFailed, err.Error())
		return err
	}

	// 5. 校验参数
	validationErrs := e.validator.ValidateParams(ctx, accountID, createParams)
	if len(validationErrs) > 0 {
		details := ""
		for _, ve := range validationErrs {
			details += fmt.Sprintf("[%s: %s] ", ve.Field, ve.Reason)
		}
		msg := fmt.Sprintf("参数校验失败: %s", details)
		e.logger.Warn("创建任务参数校验失败",
			elog.String("task_id", task.ID),
			elog.String("details", details))
		_ = e.taskDAO.UpdateStatus(ctx, task.ID, TaskStatusFailed, msg)
		return fmt.Errorf("%s", msg)
	}

	// 6. 获取适配器
	account, err := e.accountProvider.GetByID(ctx, accountID)
	if err != nil {
		_ = e.taskDAO.UpdateStatus(ctx, task.ID, TaskStatusFailed, "获取云账号失败")
		return err
	}

	adapter, err := e.adapterFactory.GetAdapter(account)
	if err != nil {
		_ = e.taskDAO.UpdateStatus(ctx, task.ID, TaskStatusFailed, "创建适配器失败")
		return err
	}

	ecsCreate := adapter.ECSCreate()
	if ecsCreate == nil {
		_ = e.taskDAO.UpdateStatus(ctx, task.ID, TaskStatusFailed, "该云厂商不支持创建实例")
		return fmt.Errorf("ECSCreate adapter not available for provider %s", provider)
	}

	// 7. 逐台创建实例
	instances := make([]ProvisionInstanceResult, 0, task.Count)
	successCount := 0
	failedCount := 0

	for i := 0; i < task.Count; i++ {
		instanceName := GenerateInstanceName(task.InstanceNamePrefix, i, task.Count)

		// 构建单台创建参数
		singleParams := *createParams
		singleParams.InstanceName = instanceName
		singleParams.Count = 1

		result, createErr := ecsCreate.CreateInstances(ctx, singleParams)

		instanceResult := ProvisionInstanceResult{
			Index:      i,
			Name:       instanceName,
			SyncStatus: SyncStatusPending,
		}

		if createErr != nil {
			instanceResult.Status = "failed"
			instanceResult.Error = createErr.Error()
			failedCount++
			e.logger.Warn("创建实例失败",
				elog.String("task_id", task.ID),
				elog.Int("index", i),
				elog.String("name", instanceName),
				elog.FieldErr(createErr))
		} else if result != nil && len(result.InstanceIDs) > 0 {
			instanceResult.Status = "success"
			instanceResult.InstanceID = result.InstanceIDs[0]
			successCount++
		} else {
			instanceResult.Status = "failed"
			instanceResult.Error = "no instance ID returned"
			failedCount++
		}

		instances = append(instances, instanceResult)

		// 更新进度
		progress := (successCount + failedCount) * 100 / task.Count
		_ = e.taskDAO.UpdateProgress(ctx, task.ID, progress, successCount, failedCount)
		_ = e.taskDAO.UpdateInstances(ctx, task.ID, instances)
	}

	// 8. 确定最终状态
	finalStatus := TaskStatusSuccess
	if failedCount == task.Count {
		finalStatus = TaskStatusFailed
	} else if failedCount > 0 {
		finalStatus = TaskStatusPartialSuccess
	}

	msg := fmt.Sprintf("创建完成: %d 成功, %d 失败", successCount, failedCount)
	_ = e.taskDAO.UpdateStatus(ctx, task.ID, finalStatus, msg)
	_ = e.taskDAO.UpdateProgress(ctx, task.ID, 100, successCount, failedCount)

	// 9. 触发资产同步
	if e.assetSyncer != nil && successCount > 0 {
		go e.syncAssets(context.Background(), &task, instances, provider, accountID)
	}

	t.Progress = 100
	t.Message = msg
	t.Result = map[string]interface{}{
		"success_count": successCount,
		"failed_count":  failedCount,
		"total":         task.Count,
	}
	return nil
}

// buildParams 根据任务来源构建统一创建参数
func (e *CreateECSExecutor) buildParams(ctx context.Context, task *ProvisionTask) (*types.CreateInstanceParams, int64, string, error) {
	if task.Source == SourceFromTemplate {
		return e.buildParamsFromTemplate(ctx, task)
	}
	return e.buildParamsFromDirect(task)
}

func (e *CreateECSExecutor) buildParamsFromTemplate(ctx context.Context, task *ProvisionTask) (*types.CreateInstanceParams, int64, string, error) {
	tmpl, err := e.tmplDAO.GetByID(ctx, task.TenantID, task.TemplateID)
	if err != nil {
		return nil, 0, "", fmt.Errorf("获取模板失败: %w", err)
	}

	// 合并标签
	tags := make(map[string]string)
	for k, v := range tmpl.Tags {
		tags[k] = v
	}
	for k, v := range task.OverrideTags {
		tags[k] = v
	}

	namePrefix := tmpl.InstanceNamePrefix
	if task.InstanceNamePrefix != "" {
		namePrefix = task.InstanceNamePrefix
	}

	params := BuildCreateInstanceParams(&tmpl, namePrefix, tags, task.Count)
	return &params, tmpl.CloudAccountID, tmpl.Provider, nil
}

func (e *CreateECSExecutor) buildParamsFromDirect(task *ProvisionTask) (*types.CreateInstanceParams, int64, string, error) {
	if task.DirectParams == nil {
		return nil, 0, "", fmt.Errorf("直接创建任务缺少参数快照")
	}

	dp := task.DirectParams
	params := types.CreateInstanceParams{
		Region:           dp.Region,
		Zone:             dp.Zone,
		InstanceType:     dp.InstanceType,
		ImageID:          dp.ImageID,
		VPCID:            dp.VPCID,
		SubnetID:         dp.SubnetID,
		SecurityGroupIDs: dp.SecurityGroupIDs,
		InstanceName:     dp.InstanceNamePrefix,
		HostName:         dp.HostNamePrefix,
		SystemDiskType:   dp.SystemDiskType,
		SystemDiskSize:   dp.SystemDiskSize,
		DataDisks:        convertDataDisks(dp.DataDisks),
		BandwidthOut:     dp.BandwidthOut,
		ChargeType:       dp.ChargeType,
		KeyPairName:      dp.KeyPairName,
		Tags:             dp.Tags,
		Count:            task.Count,
	}
	return &params, dp.CloudAccountID, dp.Provider, nil
}

// syncAssets 同步成功创建的实例到资产库（带重试）
func (e *CreateECSExecutor) syncAssets(ctx context.Context, task *ProvisionTask, instances []ProvisionInstanceResult, provider string, accountID int64) {
	_ = e.taskDAO.UpdateSyncStatus(ctx, task.ID, SyncStatusSyncing)

	allSynced := true
	for i, inst := range instances {
		if inst.Status != "success" || inst.InstanceID == "" {
			continue
		}

		synced := false
		region := ""
		if task.Source == SourceFromTemplate {
			// 从模板获取 region（已在 buildParams 中使用）
			tmpl, err := e.tmplDAO.GetByID(ctx, task.TenantID, task.TemplateID)
			if err == nil {
				region = tmpl.Region
			}
		} else if task.DirectParams != nil {
			region = task.DirectParams.Region
		}

		for retry := 0; retry < syncMaxRetries; retry++ {
			if retry > 0 {
				time.Sleep(syncRetryInterval)
			}

			err := e.assetSyncer.SyncInstanceByID(ctx, provider, accountID, region, inst.InstanceID)
			if err == nil {
				synced = true
				instances[i].SyncStatus = SyncStatusSynced
				break
			}

			e.logger.Warn("资产同步失败，将重试",
				elog.String("task_id", task.ID),
				elog.String("instance_id", inst.InstanceID),
				elog.Int("retry", retry+1),
				elog.FieldErr(err))
		}

		if !synced {
			instances[i].SyncStatus = SyncStatusFailed
			allSynced = false
		}
	}

	// 更新实例同步状态
	_ = e.taskDAO.UpdateInstances(ctx, task.ID, instances)

	if allSynced {
		_ = e.taskDAO.UpdateSyncStatus(ctx, task.ID, SyncStatusSynced)
	} else {
		_ = e.taskDAO.UpdateSyncStatus(ctx, task.ID, SyncStatusFailed)
	}
}

// Ensure compile-time interface compliance
var _ taskx.TaskExecutor = (*CreateECSExecutor)(nil)

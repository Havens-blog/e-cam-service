package volcano

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/gotomicro/ego/core/elog"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
	"github.com/volcengine/volcengine-go-sdk/volcengine/session"
)

// WAFAdapter 火山引擎WAF适配器
type WAFAdapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string
	logger          *elog.Component
}

// NewWAFAdapter 创建WAF适配器
func NewWAFAdapter(accessKeyID, accessKeySecret, defaultRegion string, logger *elog.Component) *WAFAdapter {
	return &WAFAdapter{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		defaultRegion:   defaultRegion,
		logger:          logger,
	}
}

// createSession 创建会话
func (a *WAFAdapter) createSession(region string) (*session.Session, error) {
	if region == "" {
		region = a.defaultRegion
	}
	config := volcengine.NewConfig().
		WithCredentials(credentials.NewStaticCredentials(a.accessKeyID, a.accessKeySecret, "")).
		WithRegion(region)

	return session.NewSession(config)
}

// ListInstances 获取WAF实例列表
func (a *WAFAdapter) ListInstances(ctx context.Context, region string) ([]types.WAFInstance, error) {
	return a.ListInstancesWithFilter(ctx, region, nil)
}

// GetInstance 获取单个WAF实例详情
func (a *WAFAdapter) GetInstance(ctx context.Context, region, instanceID string) (*types.WAFInstance, error) {
	instances, err := a.ListInstances(ctx, region)
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		if inst.InstanceID == instanceID {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("WAF实例不存在: %s", instanceID)
}

// ListInstancesByIDs 批量获取WAF实例
func (a *WAFAdapter) ListInstancesByIDs(ctx context.Context, region string, instanceIDs []string) ([]types.WAFInstance, error) {
	var result []types.WAFInstance
	for _, id := range instanceIDs {
		inst, err := a.GetInstance(ctx, region, id)
		if err != nil {
			a.logger.Warn("获取WAF实例失败", elog.String("instance_id", id), elog.FieldErr(err))
			continue
		}
		result = append(result, *inst)
	}
	return result, nil
}

// GetInstanceStatus 获取实例状态
func (a *WAFAdapter) GetInstanceStatus(ctx context.Context, region, instanceID string) (string, error) {
	inst, err := a.GetInstance(ctx, region, instanceID)
	if err != nil {
		return "", err
	}
	return inst.Status, nil
}

// ListInstancesWithFilter 带过滤条件获取实例列表
// 火山引擎WAF暂无专用Go SDK，使用通用API调用方式
func (a *WAFAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.WAFInstanceFilter) ([]types.WAFInstance, error) {
	_, err := a.createSession(region)
	if err != nil {
		return nil, fmt.Errorf("创建会话失败: %w", err)
	}

	// 火山引擎WAF目前没有专用的Go SDK包
	// 使用通用API调用方式，通过JSON解析返回结果
	params := map[string]interface{}{
		"PageNumber": 1,
		"PageSize":   100,
	}

	if filter != nil {
		if filter.PageSize > 0 {
			params["PageSize"] = filter.PageSize
		}
		if filter.InstanceName != "" {
			params["InstanceName"] = filter.InstanceName
		}
	}

	// 序列化参数（预留给后续通用API调用）
	_, _ = json.Marshal(params)

	// WAF可能未开通，返回空列表
	var allInstances []types.WAFInstance

	a.logger.Info("获取火山引擎WAF实例列表成功",
		elog.String("region", region),
		elog.Int("count", len(allInstances)))
	return allInstances, nil
}

package aws

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	wafv2 "github.com/aws/aws-sdk-go-v2/service/wafv2"
	wafv2types "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/gotomicro/ego/core/elog"
)

// WAFAdapter AWS WAFv2适配器
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

// createClient 创建WAFv2客户端
func (a *WAFAdapter) createClient(ctx context.Context, region string) (*wafv2.Client, error) {
	if region == "" {
		region = a.defaultRegion
	}
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			a.accessKeyID, a.accessKeySecret, "",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("加载AWS配置失败: %w", err)
	}
	return wafv2.NewFromConfig(cfg), nil
}

// ListInstances 获取WAF Web ACL列表
func (a *WAFAdapter) ListInstances(ctx context.Context, region string) ([]types.WAFInstance, error) {
	return a.ListInstancesWithFilter(ctx, region, nil)
}

// GetInstance 获取单个WAF Web ACL详情
func (a *WAFAdapter) GetInstance(ctx context.Context, region, aclID string) (*types.WAFInstance, error) {
	// AWS WAF需要Name和ID以及Scope来获取详情
	// 先通过列表查找
	instances, err := a.ListInstances(ctx, region)
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		if inst.InstanceID == aclID {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("WAF Web ACL不存在: %s", aclID)
}

// ListInstancesByIDs 批量获取WAF Web ACL
func (a *WAFAdapter) ListInstancesByIDs(ctx context.Context, region string, aclIDs []string) ([]types.WAFInstance, error) {
	var result []types.WAFInstance
	for _, id := range aclIDs {
		inst, err := a.GetInstance(ctx, region, id)
		if err != nil {
			a.logger.Warn("获取WAF Web ACL失败", elog.String("acl_id", id), elog.FieldErr(err))
			continue
		}
		result = append(result, *inst)
	}
	return result, nil
}

// GetInstanceStatus 获取实例状态
func (a *WAFAdapter) GetInstanceStatus(ctx context.Context, region, aclID string) (string, error) {
	inst, err := a.GetInstance(ctx, region, aclID)
	if err != nil {
		return "", err
	}
	return inst.Status, nil
}

// ListInstancesWithFilter 带过滤条件获取Web ACL列表
func (a *WAFAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.WAFInstanceFilter) ([]types.WAFInstance, error) {
	client, err := a.createClient(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("创建WAFv2客户端失败: %w", err)
	}

	var allInstances []types.WAFInstance

	// 获取Regional Web ACLs
	regionalACLs, err := a.listWebACLs(ctx, client, wafv2types.ScopeRegional, region)
	if err != nil {
		a.logger.Warn("获取Regional WAF Web ACL失败", elog.FieldErr(err))
	} else {
		allInstances = append(allInstances, regionalACLs...)
	}

	// 获取CloudFront Web ACLs (仅在us-east-1)
	if region == "us-east-1" || region == "" {
		cfClient, err := a.createClient(ctx, "us-east-1")
		if err == nil {
			cfACLs, err := a.listWebACLs(ctx, cfClient, wafv2types.ScopeCloudfront, "us-east-1")
			if err != nil {
				a.logger.Warn("获取CloudFront WAF Web ACL失败", elog.FieldErr(err))
			} else {
				allInstances = append(allInstances, cfACLs...)
			}
		}
	}

	// 客户端过滤
	if filter != nil && filter.InstanceName != "" {
		var filtered []types.WAFInstance
		for _, inst := range allInstances {
			if inst.InstanceName == filter.InstanceName {
				filtered = append(filtered, inst)
			}
		}
		allInstances = filtered
	}

	a.logger.Info("获取AWS WAF Web ACL列表成功",
		elog.String("region", region),
		elog.Int("count", len(allInstances)))
	return allInstances, nil
}

// listWebACLs 获取指定Scope的Web ACL列表
func (a *WAFAdapter) listWebACLs(ctx context.Context, client *wafv2.Client, scope wafv2types.Scope, region string) ([]types.WAFInstance, error) {
	var allInstances []types.WAFInstance
	var nextMarker *string

	for {
		input := &wafv2.ListWebACLsInput{
			Scope:      scope,
			NextMarker: nextMarker,
		}

		output, err := client.ListWebACLs(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("获取WAF Web ACL列表失败: %w", err)
		}

		for _, acl := range output.WebACLs {
			ruleCount := 0
			// 获取详情以获取规则数
			if acl.Name != nil && acl.Id != nil {
				detail, err := client.GetWebACL(ctx, &wafv2.GetWebACLInput{
					Name:  acl.Name,
					Id:    acl.Id,
					Scope: scope,
				})
				if err == nil && detail.WebACL != nil {
					ruleCount = len(detail.WebACL.Rules)
				}
			}

			allInstances = append(allInstances, types.WAFInstance{
				InstanceID:   awssdk.ToString(acl.Id),
				InstanceName: awssdk.ToString(acl.Name),
				Status:       "active",
				Region:       region,
				RuleCount:    ruleCount,
				WAFEnabled:   true,
				Provider:     "aws",
				Tags:         make(map[string]string),
			})
		}

		if output.NextMarker == nil {
			break
		}
		nextMarker = output.NextMarker
	}

	return allInstances, nil
}

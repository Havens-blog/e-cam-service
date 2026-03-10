package volcano

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/gotomicro/ego/core/elog"
	"github.com/volcengine/volcengine-go-sdk/service/cdn"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
	"github.com/volcengine/volcengine-go-sdk/volcengine/session"
)

// CDNAdapter 火山引擎CDN适配器
type CDNAdapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string
	logger          *elog.Component
}

// NewCDNAdapter 创建CDN适配器
func NewCDNAdapter(accessKeyID, accessKeySecret, defaultRegion string, logger *elog.Component) *CDNAdapter {
	return &CDNAdapter{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		defaultRegion:   defaultRegion,
		logger:          logger,
	}
}

// createClient 创建CDN客户端
func (a *CDNAdapter) createClient() (*cdn.CDN, error) {
	config := volcengine.NewConfig().
		WithCredentials(credentials.NewStaticCredentials(a.accessKeyID, a.accessKeySecret, "")).
		WithRegion(a.defaultRegion)

	sess, err := session.NewSession(config)
	if err != nil {
		return nil, fmt.Errorf("创建会话失败: %w", err)
	}

	return cdn.New(sess), nil
}

// ListInstances 获取CDN加速域名列表
func (a *CDNAdapter) ListInstances(ctx context.Context, region string) ([]types.CDNInstance, error) {
	return a.ListInstancesWithFilter(ctx, region, nil)
}

// GetInstance 获取单个CDN加速域名详情
func (a *CDNAdapter) GetInstance(ctx context.Context, region, domainName string) (*types.CDNInstance, error) {
	client, err := a.createClient()
	if err != nil {
		return nil, fmt.Errorf("创建CDN客户端失败: %w", err)
	}

	input := &cdn.DescribeCdnConfigInput{}
	input.SetDomain(domainName)

	output, err := client.DescribeCdnConfig(input)
	if err != nil {
		return nil, fmt.Errorf("获取CDN域名详情失败: %w", err)
	}

	if output.DomainConfig == nil {
		return nil, fmt.Errorf("CDN域名不存在: %s", domainName)
	}

	instance := a.convertDetailToInstance(output.DomainConfig)
	return &instance, nil
}

// ListInstancesByIDs 批量获取CDN加速域名
func (a *CDNAdapter) ListInstancesByIDs(ctx context.Context, region string, domainNames []string) ([]types.CDNInstance, error) {
	var result []types.CDNInstance
	for _, name := range domainNames {
		inst, err := a.GetInstance(ctx, region, name)
		if err != nil {
			a.logger.Warn("获取CDN域名失败", elog.String("domain", name), elog.FieldErr(err))
			continue
		}
		result = append(result, *inst)
	}
	return result, nil
}

// GetInstanceStatus 获取域名状态
func (a *CDNAdapter) GetInstanceStatus(ctx context.Context, region, domainName string) (string, error) {
	inst, err := a.GetInstance(ctx, region, domainName)
	if err != nil {
		return "", err
	}
	return inst.Status, nil
}

// ListInstancesWithFilter 带过滤条件获取域名列表
func (a *CDNAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.CDNInstanceFilter) ([]types.CDNInstance, error) {
	client, err := a.createClient()
	if err != nil {
		return nil, fmt.Errorf("创建CDN客户端失败: %w", err)
	}

	var allInstances []types.CDNInstance
	pageNum := int64(1)
	pageSize := int64(100)

	if filter != nil && filter.PageSize > 0 {
		pageSize = int64(filter.PageSize)
	}

	for {
		input := &cdn.ListCdnDomainsInput{}
		input.SetPageNum(pageNum)
		input.SetPageSize(pageSize)

		if filter != nil {
			if filter.DomainName != "" {
				input.SetDomain(filter.DomainName)
			}
			if filter.Status != "" {
				input.SetStatus(filter.Status)
			}
		}

		output, err := client.ListCdnDomains(input)
		if err != nil {
			return nil, fmt.Errorf("获取CDN域名列表失败: %w", err)
		}

		if output.Data == nil || len(output.Data) == 0 {
			break
		}

		for _, d := range output.Data {
			allInstances = append(allInstances, a.convertToInstance(d))
		}

		if int64(len(output.Data)) < pageSize {
			break
		}
		pageNum++
	}

	a.logger.Info("获取火山引擎CDN域名列表成功", elog.Int("count", len(allInstances)))
	return allInstances, nil
}

// convertToInstance 转换列表项为通用CDN实例
func (a *CDNAdapter) convertToInstance(d *cdn.DataForListCdnDomainsOutput) types.CDNInstance {
	domainName := ""
	if d.Domain != nil {
		domainName = *d.Domain
	}
	cname := ""
	if d.Cname != nil {
		cname = *d.Cname
	}
	status := ""
	if d.Status != nil {
		status = *d.Status
	}
	serviceType := ""
	if d.ServiceType != nil {
		serviceType = *d.ServiceType
	}
	serviceRegion := ""
	if d.ServiceRegion != nil {
		serviceRegion = *d.ServiceRegion
	}
	createTime := ""
	if d.CreateTime != nil {
		createTime = fmt.Sprintf("%d", *d.CreateTime)
	}
	updateTime := ""
	if d.UpdateTime != nil {
		updateTime = fmt.Sprintf("%d", *d.UpdateTime)
	}

	httpsEnabled := false
	if d.HTTPS != nil && *d.HTTPS {
		httpsEnabled = true
	}

	origins := make([]types.CDNOrigin, 0)
	if d.PrimaryOrigin != nil {
		for _, o := range d.PrimaryOrigin {
			if o != nil {
				origins = append(origins, types.CDNOrigin{
					Address: *o,
					Type:    "domain",
				})
			}
		}
	}

	return types.CDNInstance{
		DomainName:   domainName,
		Cname:        cname,
		Status:       status,
		BusinessType: serviceType,
		ServiceArea:  serviceRegion,
		Origins:      origins,
		HTTPSEnabled: httpsEnabled,
		CreationTime: createTime,
		ModifiedTime: updateTime,
		Provider:     "volcano",
		Tags:         make(map[string]string),
	}
}

// convertDetailToInstance 转换详情为通用CDN实例
func (a *CDNAdapter) convertDetailToInstance(d *cdn.DomainConfigForDescribeCdnConfigOutput) types.CDNInstance {
	domainName := ""
	if d.Domain != nil {
		domainName = *d.Domain
	}
	cname := ""
	if d.Cname != nil {
		cname = *d.Cname
	}
	status := ""
	if d.Status != nil {
		status = *d.Status
	}
	serviceType := ""
	if d.ServiceType != nil {
		serviceType = *d.ServiceType
	}
	serviceRegion := ""
	if d.ServiceRegion != nil {
		serviceRegion = *d.ServiceRegion
	}
	createTime := ""
	if d.CreateTime != nil {
		createTime = fmt.Sprintf("%d", *d.CreateTime)
	}
	updateTime := ""
	if d.UpdateTime != nil {
		updateTime = fmt.Sprintf("%d", *d.UpdateTime)
	}

	origins := make([]types.CDNOrigin, 0)
	if d.OriginHost != nil {
		origins = append(origins, types.CDNOrigin{
			Address: *d.OriginHost,
			Type:    "domain",
		})
	}

	return types.CDNInstance{
		DomainName:   domainName,
		Cname:        cname,
		Status:       status,
		BusinessType: serviceType,
		ServiceArea:  serviceRegion,
		Origins:      origins,
		CreationTime: createTime,
		ModifiedTime: updateTime,
		Provider:     "volcano",
		Tags:         make(map[string]string),
	}
}

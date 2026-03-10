package aliyun

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/cdn"
	"github.com/gotomicro/ego/core/elog"
)

// CDNAdapter 阿里云CDN适配器
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
func (a *CDNAdapter) createClient() (*cdn.Client, error) {
	return cdn.NewClientWithAccessKey(a.defaultRegion, a.accessKeyID, a.accessKeySecret)
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

	request := cdn.CreateDescribeCdnDomainDetailRequest()
	request.DomainName = domainName

	response, err := client.DescribeCdnDomainDetail(request)
	if err != nil {
		return nil, fmt.Errorf("获取CDN域名详情失败: %w", err)
	}

	detail := response.GetDomainDetailModel
	origins := make([]types.CDNOrigin, 0)
	for _, src := range detail.SourceModels.SourceModel {
		priority, _ := strconv.Atoi(src.Priority)
		weight, _ := strconv.Atoi(src.Weight)
		origins = append(origins, types.CDNOrigin{
			Address:  src.Content,
			Type:     src.Type,
			Port:     src.Port,
			Priority: priority,
			Weight:   weight,
		})
	}

	instance := types.CDNInstance{
		DomainName:      detail.DomainName,
		Cname:           detail.Cname,
		Status:          detail.DomainStatus,
		BusinessType:    detail.CdnType,
		Origins:         origins,
		ResourceGroupID: detail.ResourceGroupId,
		CreationTime:    detail.GmtCreated,
		ModifiedTime:    detail.GmtModified,
		Description:     detail.Description,
		Provider:        "aliyun",
	}
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
	pageNumber := 1
	pageSize := 50

	if filter != nil && filter.PageSize > 0 {
		pageSize = filter.PageSize
	}

	for {
		request := cdn.CreateDescribeUserDomainsRequest()
		request.PageNumber = requests.NewInteger(pageNumber)
		request.PageSize = requests.NewInteger(pageSize)

		if filter != nil {
			if filter.DomainName != "" {
				request.DomainName = filter.DomainName
			}
			if filter.Status != "" {
				request.DomainStatus = filter.Status
			}
		}

		response, err := client.DescribeUserDomains(request)
		if err != nil {
			return nil, fmt.Errorf("获取CDN域名列表失败: %w", err)
		}

		for _, d := range response.Domains.PageData {
			origins := make([]types.CDNOrigin, 0)
			for _, src := range d.Sources.Source {
				priority, _ := strconv.Atoi(src.Priority)
				weight, _ := strconv.Atoi(src.Weight)
				origins = append(origins, types.CDNOrigin{
					Address:  src.Content,
					Type:     src.Type,
					Port:     src.Port,
					Priority: priority,
					Weight:   weight,
				})
			}

			allInstances = append(allInstances, types.CDNInstance{
				DomainName:      d.DomainName,
				Cname:           d.Cname,
				Status:          d.DomainStatus,
				Region:          d.Coverage,
				BusinessType:    d.CdnType,
				ServiceArea:     d.Coverage,
				Origins:         origins,
				ResourceGroupID: d.ResourceGroupId,
				CreationTime:    d.GmtCreated,
				ModifiedTime:    d.GmtModified,
				Description:     d.Description,
				Provider:        "aliyun",
			})
		}

		if len(response.Domains.PageData) < pageSize {
			break
		}
		pageNumber++
	}

	a.logger.Info("获取阿里云CDN域名列表成功", elog.Int("count", len(allInstances)))
	return allInstances, nil
}

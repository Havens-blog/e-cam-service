package template

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// AccountProvider 云账号查询接口（解耦 repository 依赖）
type AccountProvider interface {
	GetByID(ctx context.Context, id int64) (*domain.CloudAccount, error)
}

// AdapterFactory 适配器工厂接口
type AdapterFactory interface {
	GetAdapter(account *domain.CloudAccount) (cloudx.CloudAdapter, error)
}

// TemplateValidator 模板参数校验器
// 统一校验两种创建方式（模板创建和直接创建）的参数
type TemplateValidator struct {
	accountProvider AccountProvider
	adapterFactory  AdapterFactory
}

// NewTemplateValidator 创建校验器
func NewTemplateValidator(accountProvider AccountProvider, adapterFactory AdapterFactory) *TemplateValidator {
	return &TemplateValidator{
		accountProvider: accountProvider,
		adapterFactory:  adapterFactory,
	}
}

// ValidateParams 校验创建参数的完整性和合法性
// 返回所有校验失败项的列表，空列表表示校验通过
func (v *TemplateValidator) ValidateParams(ctx context.Context, accountID int64, params *types.CreateInstanceParams) []types.ValidationError {
	var errs []types.ValidationError

	// 1. 校验必填参数
	errs = append(errs, v.validateRequired(params)...)
	if len(errs) > 0 {
		return errs
	}

	// 2. 校验云账号状态
	account, err := v.accountProvider.GetByID(ctx, accountID)
	if err != nil {
		errs = append(errs, types.ValidationError{
			Field:  "cloud_account_id",
			Reason: "cloud account not found",
		})
		return errs
	}
	if account.Status != "enabled" {
		errs = append(errs, types.ValidationError{
			Field:  "cloud_account_id",
			Reason: "cloud account is disabled",
		})
		return errs
	}

	// 3. 获取适配器
	adapter, err := v.adapterFactory.GetAdapter(account)
	if err != nil {
		errs = append(errs, types.ValidationError{
			Field:  "provider",
			Reason: "failed to create cloud adapter: " + err.Error(),
		})
		return errs
	}

	// 4. 校验云资源（如果 ResourceQuery 适配器可用）
	rq := adapter.ResourceQuery()
	if rq != nil {
		errs = append(errs, v.validateCloudResources(ctx, rq, params)...)
	}

	return errs
}

// validateRequired 校验必填参数
func (v *TemplateValidator) validateRequired(params *types.CreateInstanceParams) []types.ValidationError {
	var errs []types.ValidationError

	if params.Region == "" {
		errs = append(errs, types.ValidationError{Field: "region", Reason: "region is required"})
	}
	if params.Zone == "" {
		errs = append(errs, types.ValidationError{Field: "zone", Reason: "zone is required"})
	}
	if params.InstanceType == "" {
		errs = append(errs, types.ValidationError{Field: "instance_type", Reason: "instance_type is required"})
	}
	if params.ImageID == "" {
		errs = append(errs, types.ValidationError{Field: "image_id", Reason: "image_id is required"})
	}
	if params.VPCID == "" {
		errs = append(errs, types.ValidationError{Field: "vpc_id", Reason: "vpc_id is required"})
	}
	if params.SubnetID == "" {
		errs = append(errs, types.ValidationError{Field: "subnet_id", Reason: "subnet_id is required"})
	}
	if len(params.SecurityGroupIDs) == 0 {
		errs = append(errs, types.ValidationError{Field: "security_group_ids", Reason: "at least one security group is required"})
	}
	if params.Count < 1 || params.Count > 20 {
		errs = append(errs, types.ValidationError{Field: "count", Reason: "count must be between 1 and 20"})
	}

	return errs
}

// validateCloudResources 通过云厂商 API 校验资源有效性
func (v *TemplateValidator) validateCloudResources(ctx context.Context, rq cloudx.ResourceQueryAdapter, params *types.CreateInstanceParams) []types.ValidationError {
	var errs []types.ValidationError

	// 校验实例规格
	instanceTypes, err := rq.ListAvailableInstanceTypes(ctx, params.Region)
	if err == nil {
		found := false
		for _, it := range instanceTypes {
			if it.InstanceType == params.InstanceType {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, types.ValidationError{
				Field:  "instance_type",
				Reason: "instance type not available in the specified region",
			})
		}
	}

	// 校验镜像
	images, err := rq.ListAvailableImages(ctx, params.Region)
	if err == nil {
		found := false
		for _, img := range images {
			if img.ImageID == params.ImageID {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, types.ValidationError{
				Field:  "image_id",
				Reason: "image not found in the specified region",
			})
		}
	}

	// 校验 VPC
	vpcs, err := rq.ListVPCs(ctx, params.Region)
	if err == nil {
		found := false
		for _, vpc := range vpcs {
			if vpc.VPCID == params.VPCID {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, types.ValidationError{
				Field:  "vpc_id",
				Reason: "VPC not found in the specified region",
			})
		}
	}

	// 校验子网（需属于指定 VPC）
	subnets, err := rq.ListSubnets(ctx, params.Region, params.VPCID)
	if err == nil {
		found := false
		for _, sn := range subnets {
			if sn.SubnetID == params.SubnetID {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, types.ValidationError{
				Field:  "subnet_id",
				Reason: "subnet not found in the specified VPC",
			})
		}
	}

	// 校验安全组（需属于指定 VPC）
	secGroups, err := rq.ListSecurityGroups(ctx, params.Region, params.VPCID)
	if err == nil {
		sgMap := make(map[string]bool)
		for _, sg := range secGroups {
			sgMap[sg.SecurityGroupID] = true
		}
		for _, sgID := range params.SecurityGroupIDs {
			if !sgMap[sgID] {
				errs = append(errs, types.ValidationError{
					Field:  "security_group_ids",
					Reason: "security group " + sgID + " not found in the specified VPC",
				})
			}
		}
	}

	return errs
}

package template

import (
	"context"
	"fmt"
	"strings"

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

	// 只校验必填参数，不做云资源有效性校验（避免 API 列表不全导致误报）
	errs = append(errs, v.validateRequired(params)...)
	if len(errs) > 0 {
		return errs
	}

	// 校验云账号状态
	account, err := v.accountProvider.GetByID(ctx, accountID)
	if err != nil {
		errs = append(errs, types.ValidationError{
			Field:  "cloud_account_id",
			Reason: "cloud account not found",
		})
		return errs
	}
	if account.Status != "enabled" && account.Status != "active" {
		errs = append(errs, types.ValidationError{
			Field:  "cloud_account_id",
			Reason: "cloud account is not active (status: " + string(account.Status) + ")",
		})
	}

	// 校验镜像架构与实例规格架构的兼容性
	if params.ImageID != "" && params.InstanceType != "" {
		errs = append(errs, v.validateArchitectureCompat(ctx, account, params)...)
	}

	// 校验实例规格在目标地域是否可用
	if params.InstanceType != "" && len(errs) == 0 {
		adapter, adapterErr := v.adapterFactory.GetAdapter(account)
		if adapterErr == nil {
			rq := adapter.ResourceQuery()
			if rq == nil {
				rq = cloudx.NewGenericResourceQueryAdapter(adapter)
			}
			errs = append(errs, v.validateCloudResources(ctx, rq, params)...)
		}
	}

	return errs
}

// validateRequired 校验必填参数（只校验最核心的字段）
func (v *TemplateValidator) validateRequired(params *types.CreateInstanceParams) []types.ValidationError {
	var errs []types.ValidationError

	if params.Region == "" {
		errs = append(errs, types.ValidationError{Field: "region", Reason: "region is required"})
	}
	if params.InstanceType == "" {
		errs = append(errs, types.ValidationError{Field: "instance_type", Reason: "instance_type is required"})
	}
	if params.Count < 1 || params.Count > 20 {
		errs = append(errs, types.ValidationError{Field: "count", Reason: "count must be between 1 and 20"})
	}

	return errs
}

// validateArchitectureCompat 校验镜像架构与实例规格架构的兼容性
func (v *TemplateValidator) validateArchitectureCompat(ctx context.Context, account *domain.CloudAccount, params *types.CreateInstanceParams) []types.ValidationError {
	adapter, err := v.adapterFactory.GetAdapter(account)
	if err != nil {
		return nil // 无法获取适配器时跳过校验，让云 API 返回具体错误
	}

	rq := adapter.ResourceQuery()
	if rq == nil {
		return nil
	}

	// 查询镜像架构
	images, err := rq.ListAvailableImages(ctx, params.Region)
	if err != nil {
		return nil
	}

	var imageArch string
	for _, img := range images {
		if img.ImageID == params.ImageID {
			imageArch = img.Architecture
			break
		}
	}
	if imageArch == "" {
		return nil // 镜像未找到或无架构信息，跳过校验
	}

	// 查询实例规格架构
	instanceTypes, err := rq.ListAvailableInstanceTypes(ctx, params.Region)
	if err != nil {
		return nil
	}

	var instanceArch string
	for _, it := range instanceTypes {
		if it.InstanceType == params.InstanceType {
			instanceArch = it.Architecture
			break
		}
	}
	if instanceArch == "" {
		return nil // 规格未找到或无架构信息，跳过校验
	}

	// 架构兼容性检查: x86_64 对 X86, arm64 对 ARM
	if !isArchCompatible(imageArch, instanceArch) {
		return []types.ValidationError{{
			Field:  "image_id",
			Reason: fmt.Sprintf("镜像架构(%s)与实例规格架构(%s)不兼容，请选择匹配的镜像或实例规格", imageArch, instanceArch),
		}}
	}

	return nil
}

// isArchCompatible 判断镜像架构和实例规格架构是否兼容
func isArchCompatible(imageArch, instanceArch string) bool {
	imageArch = strings.ToLower(imageArch)
	instanceArch = strings.ToLower(instanceArch)

	// 完全相同
	if imageArch == instanceArch {
		return true
	}

	// x86 系列兼容
	x86Set := map[string]bool{"x86_64": true, "x86": true, "amd64": true, "i386": true}
	if x86Set[imageArch] && x86Set[instanceArch] {
		return true
	}

	// ARM 系列兼容
	armSet := map[string]bool{"arm64": true, "aarch64": true, "arm": true}
	if armSet[imageArch] && armSet[instanceArch] {
		return true
	}

	return false
}

// validateCloudResources 通过云厂商 API 校验资源有效性（仅校验非空字段）
func (v *TemplateValidator) validateCloudResources(ctx context.Context, rq cloudx.ResourceQueryAdapter, params *types.CreateInstanceParams) []types.ValidationError {
	var errs []types.ValidationError

	// 校验实例规格（仅当有值时）
	if params.InstanceType != "" {
		instanceTypes, err := rq.ListAvailableInstanceTypes(ctx, params.Region)
		if err == nil && len(instanceTypes) > 0 {
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
					Reason: fmt.Sprintf("实例规格 %s 在地域 %s 不可用，请选择该地域支持的规格", params.InstanceType, params.Region),
				})
			}
		}
	}

	// 其他资源校验跳过（image/vpc/subnet/sg 为可选字段，不强制校验）
	return errs
}

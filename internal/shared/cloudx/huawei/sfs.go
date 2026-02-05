package huawei

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	sfs "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/sfsturbo/v1"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/sfsturbo/v1/model"
	sfsregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/sfsturbo/v1/region"
)

// SFSAdapter 华为云 SFS Turbo (弹性文件服务) 适配器
type SFSAdapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string
	logger          *elog.Component
}

// NewSFSAdapter 创建 SFS 适配器
func NewSFSAdapter(accessKeyID, accessKeySecret, defaultRegion string, logger *elog.Component) *SFSAdapter {
	return &SFSAdapter{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		defaultRegion:   defaultRegion,
		logger:          logger,
	}
}

// createClient 创建 SFS 客户端
func (a *SFSAdapter) createClient(region string) (*sfs.SFSTurboClient, error) {
	if region == "" {
		region = a.defaultRegion
	}

	auth, err := basic.NewCredentialsBuilder().
		WithAk(a.accessKeyID).
		WithSk(a.accessKeySecret).
		SafeBuild()
	if err != nil {
		return nil, fmt.Errorf("创建认证凭证失败: %w", err)
	}

	regionObj, err := sfsregion.SafeValueOf(region)
	if err != nil {
		// 如果 region 不在预定义列表中，使用默认 region
		regionObj = sfsregion.CN_NORTH_4
	}

	client, err := sfs.SFSTurboClientBuilder().
		WithRegion(regionObj).
		WithCredential(auth).
		SafeBuild()
	if err != nil {
		return nil, fmt.Errorf("创建SFS客户端失败: %w", err)
	}

	return sfs.NewSFSTurboClient(client), nil
}

// ListInstances 获取 SFS 文件系统列表
func (a *SFSAdapter) ListInstances(ctx context.Context, region string) ([]types.NASInstance, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建SFS客户端失败: %w", err)
	}

	var instances []types.NASInstance
	offset := int32(0)
	limit := int32(100)

	for {
		request := &model.ListSharesRequest{
			Offset: &offset,
			Limit:  &limit,
		}

		response, err := client.ListShares(request)
		if err != nil {
			return nil, fmt.Errorf("获取SFS文件系统列表失败: %w", err)
		}

		if response.Shares == nil || len(*response.Shares) == 0 {
			break
		}

		for _, share := range *response.Shares {
			instance := a.convertToNASInstance(&share, region)
			instances = append(instances, instance)
		}

		if len(*response.Shares) < int(limit) {
			break
		}
		offset += limit
	}

	return instances, nil
}

// GetInstance 获取单个 SFS 文件系统详情
func (a *SFSAdapter) GetInstance(ctx context.Context, region, fileSystemID string) (*types.NASInstance, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建SFS客户端失败: %w", err)
	}

	request := &model.ShowShareRequest{
		ShareId: fileSystemID,
	}

	response, err := client.ShowShare(request)
	if err != nil {
		return nil, fmt.Errorf("获取SFS文件系统详情失败: %w", err)
	}

	instance := a.convertDetailToNASInstance(response, region)
	return &instance, nil
}

// ListInstancesByIDs 批量获取 SFS 文件系统
func (a *SFSAdapter) ListInstancesByIDs(ctx context.Context, region string, fileSystemIDs []string) ([]types.NASInstance, error) {
	var instances []types.NASInstance
	for _, id := range fileSystemIDs {
		instance, err := a.GetInstance(ctx, region, id)
		if err != nil {
			a.logger.Warn("获取SFS文件系统失败", elog.String("file_system_id", id), elog.FieldErr(err))
			continue
		}
		instances = append(instances, *instance)
	}
	return instances, nil
}

// GetInstanceStatus 获取文件系统状态
func (a *SFSAdapter) GetInstanceStatus(ctx context.Context, region, fileSystemID string) (string, error) {
	instance, err := a.GetInstance(ctx, region, fileSystemID)
	if err != nil {
		return "", err
	}
	return instance.Status, nil
}

// ListInstancesWithFilter 带过滤条件获取文件系统列表
func (a *SFSAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.NASInstanceFilter) ([]types.NASInstance, error) {
	instances, err := a.ListInstances(ctx, region)
	if err != nil {
		return nil, err
	}

	if filter == nil {
		return instances, nil
	}

	var filtered []types.NASInstance
	for _, inst := range instances {
		// 按状态过滤
		if len(filter.Status) > 0 && !containsString(filter.Status, inst.Status) {
			continue
		}
		// 按文件系统类型过滤
		if filter.FileSystemType != "" && inst.FileSystemType != filter.FileSystemType {
			continue
		}
		// 按协议类型过滤
		if filter.ProtocolType != "" && inst.ProtocolType != filter.ProtocolType {
			continue
		}
		filtered = append(filtered, inst)
	}

	return filtered, nil
}

// convertToNASInstance 转换为统一的 NAS 实例结构
func (a *SFSAdapter) convertToNASInstance(share *model.ShareInfo, region string) types.NASInstance {
	instance := types.NASInstance{
		Region:   region,
		Provider: string(domain.CloudProviderHuawei),
	}

	if share.Id != nil {
		instance.FileSystemID = *share.Id
	}
	if share.Name != nil {
		instance.FileSystemName = *share.Name
		instance.Description = *share.Name
	}
	if share.Status != nil {
		instance.Status = a.convertStatus(*share.Status)
	}
	if share.AvailabilityZone != nil {
		instance.Zone = *share.AvailabilityZone
	}
	if share.ShareType != nil {
		// ShareType: STANDARD(标准型) / PERFORMANCE(性能型)
		instance.FileSystemType = *share.ShareType
		// 同时作为存储类型
		instance.StorageType = *share.ShareType
	}
	if share.ShareProto != nil {
		instance.ProtocolType = *share.ShareProto // NFS / CIFS
	}

	// Size 是总容量 (GB)，AvailCapacity 是剩余容量 (GB)
	var totalCapacity, availCapacity int64
	if share.Size != nil {
		if size, err := strconv.ParseInt(*share.Size, 10, 64); err == nil {
			totalCapacity = size
			instance.Capacity = size
		}
	}
	if share.AvailCapacity != nil {
		// AvailCapacity 可能是浮点数字符串，如 "500.00"
		if avail, err := strconv.ParseFloat(*share.AvailCapacity, 64); err == nil {
			availCapacity = int64(avail)
		}
	}
	// 计算已用容量 = 总容量 - 剩余容量
	if totalCapacity > 0 && availCapacity >= 0 {
		instance.UsedCapacity = totalCapacity - availCapacity
		// MeteredSize 转换为字节 (GB -> Bytes)
		instance.MeteredSize = instance.UsedCapacity * 1024 * 1024 * 1024
	}

	// VPC 和子网信息
	if share.VpcId != nil {
		instance.VPCID = *share.VpcId
	}
	if share.SubnetId != nil {
		instance.VSwitchID = *share.SubnetId
	}

	// 挂载点信息
	if share.ExportLocation != nil && *share.ExportLocation != "" {
		instance.MountTargets = []types.MountTarget{
			{
				MountTargetDomain: *share.ExportLocation,
				NetworkType:       "VPC",
				VPCID:             instance.VPCID,
				VSwitchID:         instance.VSwitchID,
				Status:            instance.Status,
			},
		}
	}

	// 计费模式
	if share.PayModel != nil {
		payModelStr := share.PayModel.Value()
		if payModelStr == "0" {
			instance.ChargeType = "PostPaid" // 按需付费
		} else {
			instance.ChargeType = "PrePaid" // 包周期
		}
	}

	// 创建时间
	if share.CreatedAt != nil {
		instance.CreationTime = time.Time(*share.CreatedAt)
	}

	// 加密信息 - 华为云 SFS Turbo 通过 CryptKeyId 判断是否加密
	if share.CryptKeyId != nil && *share.CryptKeyId != "" {
		instance.EncryptType = 1 // 加密
		instance.KMSKeyID = *share.CryptKeyId
	} else {
		instance.EncryptType = 0 // 不加密
	}

	// 标签
	if share.Tags != nil && len(*share.Tags) > 0 {
		instance.Tags = make(map[string]string)
		for _, tag := range *share.Tags {
			if tag.Key != "" {
				instance.Tags[tag.Key] = tag.Value
			}
		}
	}

	return instance
}

// convertDetailToNASInstance 从详情转换
func (a *SFSAdapter) convertDetailToNASInstance(response *model.ShowShareResponse, region string) types.NASInstance {
	instance := types.NASInstance{
		Region:   region,
		Provider: string(domain.CloudProviderHuawei),
	}

	if response.Id != nil {
		instance.FileSystemID = *response.Id
	}
	if response.Name != nil {
		instance.FileSystemName = *response.Name
		instance.Description = *response.Name
	}
	if response.Status != nil {
		instance.Status = a.convertStatus(*response.Status)
	}
	if response.AvailabilityZone != nil {
		instance.Zone = *response.AvailabilityZone
	}
	if response.ShareType != nil {
		// ShareType: STANDARD(标准型) / PERFORMANCE(性能型)
		instance.FileSystemType = *response.ShareType
		instance.StorageType = *response.ShareType
	}
	if response.ShareProto != nil {
		instance.ProtocolType = *response.ShareProto // NFS / CIFS
	}

	// Size 是总容量 (GB)，AvailCapacity 是剩余容量 (GB)
	var totalCapacity, availCapacity int64
	if response.Size != nil {
		if size, err := strconv.ParseInt(*response.Size, 10, 64); err == nil {
			totalCapacity = size
			instance.Capacity = size
		}
	}
	if response.AvailCapacity != nil {
		// AvailCapacity 可能是浮点数字符串，如 "500.00"
		if avail, err := strconv.ParseFloat(*response.AvailCapacity, 64); err == nil {
			availCapacity = int64(avail)
		}
	}
	// 计算已用容量 = 总容量 - 剩余容量
	if totalCapacity > 0 && availCapacity >= 0 {
		instance.UsedCapacity = totalCapacity - availCapacity
		// MeteredSize 转换为字节 (GB -> Bytes)
		instance.MeteredSize = instance.UsedCapacity * 1024 * 1024 * 1024
	}

	// VPC 和子网信息
	if response.VpcId != nil {
		instance.VPCID = *response.VpcId
	}
	if response.SubnetId != nil {
		instance.VSwitchID = *response.SubnetId
	}

	// 挂载点信息
	if response.ExportLocation != nil && *response.ExportLocation != "" {
		instance.MountTargets = []types.MountTarget{
			{
				MountTargetDomain: *response.ExportLocation,
				NetworkType:       "VPC",
				VPCID:             instance.VPCID,
				VSwitchID:         instance.VSwitchID,
				Status:            instance.Status,
			},
		}
	}

	// 计费模式
	if response.PayModel != nil {
		payModelStr := response.PayModel.Value()
		if payModelStr == "0" {
			instance.ChargeType = "PostPaid" // 按需付费
		} else {
			instance.ChargeType = "PrePaid" // 包周期
		}
	}

	// 创建时间
	if response.CreatedAt != nil {
		instance.CreationTime = time.Time(*response.CreatedAt)
	}

	// 加密信息
	if response.CryptKeyId != nil && *response.CryptKeyId != "" {
		instance.EncryptType = 1
		instance.KMSKeyID = *response.CryptKeyId
	} else {
		instance.EncryptType = 0
	}

	// 标签
	if response.Tags != nil && len(*response.Tags) > 0 {
		instance.Tags = make(map[string]string)
		for _, tag := range *response.Tags {
			if tag.Key != "" {
				instance.Tags[tag.Key] = tag.Value
			}
		}
	}

	return instance
}

// convertStatus 转换状态
func (a *SFSAdapter) convertStatus(status string) string {
	statusMap := map[string]string{
		"100":       "creating",
		"200":       "running",
		"300":       "deleting",
		"303":       "error",
		"400":       "deleted",
		"available": "running",
		"creating":  "creating",
		"deleting":  "deleting",
		"error":     "error",
	}
	if s, ok := statusMap[status]; ok {
		return s
	}
	return status
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

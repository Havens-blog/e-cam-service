package template

import (
	"context"
	"errors"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/mongo"
)

// 主机模板相关错误码
var (
	ErrTemplateNotFound   = errs.ErrorCode{Code: 404030, Msg: "vm template not found"}
	ErrTemplateNameExists = errs.ErrorCode{Code: 409030, Msg: "vm template name already exists"}
	ErrTemplateInUse      = errs.ErrorCode{Code: 400030, Msg: "vm template has running tasks, cannot delete"}
	ErrInvalidCount       = errs.ErrorCode{Code: 400031, Msg: "count must be between 1 and 20"}
	ErrValidationFailed   = errs.ErrorCode{Code: 400032, Msg: "parameter validation failed"}
	ErrCloudAPIError      = errs.ErrorCode{Code: 500030, Msg: "cloud api error"}
)

// TaskSubmitter 任务提交接口（解耦 taskx 依赖）
type TaskSubmitter interface {
	Submit(ctx context.Context, taskType string, payload interface{}) error
}

// TemplateService 模板服务接口
type TemplateService interface {
	// 模板 CRUD
	CreateTemplate(ctx context.Context, tenantID string, req CreateTemplateReq) (*VMTemplate, error)
	GetTemplate(ctx context.Context, tenantID string, id int64) (*VMTemplate, error)
	ListTemplates(ctx context.Context, tenantID string, filter TemplateFilter) ([]VMTemplate, int64, error)
	UpdateTemplate(ctx context.Context, tenantID string, id int64, req UpdateTemplateReq) error
	DeleteTemplate(ctx context.Context, tenantID string, id int64) error

	// 两种创建方式
	ProvisionFromTemplate(ctx context.Context, tenantID, createdBy string, templateID int64, req ProvisionReq) (string, error)
	DirectProvision(ctx context.Context, tenantID, createdBy string, req DirectProvisionReq) (string, error)

	// 任务查询
	ListProvisionTasks(ctx context.Context, tenantID string, filter ProvisionTaskFilter) ([]ProvisionTask, int64, error)
	GetProvisionTask(ctx context.Context, tenantID, taskID string) (*ProvisionTask, error)
}

// templateService TemplateService 实现
type templateService struct {
	tmplDAO   TemplateDAO
	taskDAO   ProvisionTaskDAO
	submitter TaskSubmitter
}

// NewTemplateService 创建 TemplateService 实例
func NewTemplateService(tmplDAO TemplateDAO, taskDAO ProvisionTaskDAO, submitter TaskSubmitter) TemplateService {
	return &templateService{
		tmplDAO:   tmplDAO,
		taskDAO:   taskDAO,
		submitter: submitter,
	}
}

// ==================== 模板 CRUD ====================

func (s *templateService) CreateTemplate(ctx context.Context, tenantID string, req CreateTemplateReq) (*VMTemplate, error) {
	// 参数校验：只有 name 是必填的，其他字段允许为空（模板可以部分填写后续补充）
	if req.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidationFailed)
	}

	// 校验名称唯一性
	_, err := s.tmplDAO.GetByName(ctx, tenantID, req.Name)
	if err == nil {
		return nil, ErrTemplateNameExists
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}

	tmpl := VMTemplate{
		Name:               req.Name,
		Description:        req.Description,
		TenantID:           tenantID,
		Provider:           req.Provider,
		CloudAccountID:     req.CloudAccountID,
		Region:             req.Region,
		Zone:               req.Zone,
		InstanceType:       req.InstanceType,
		ImageID:            req.ImageID,
		VPCID:              req.VPCID,
		SubnetID:           req.SubnetID,
		SecurityGroupIDs:   req.SecurityGroupIDs,
		InstanceNamePrefix: req.InstanceNamePrefix,
		HostNamePrefix:     req.HostNamePrefix,
		SystemDiskType:     req.SystemDiskType,
		SystemDiskSize:     req.SystemDiskSize,
		DataDisks:          req.DataDisks,
		BandwidthOut:       req.BandwidthOut,
		ChargeType:         req.ChargeType,
		KeyPairName:        req.KeyPairName,
		Tags:               req.Tags,
	}

	id, err := s.tmplDAO.Insert(ctx, tmpl)
	if err != nil {
		return nil, err
	}
	tmpl.ID = id
	return &tmpl, nil
}

func (s *templateService) GetTemplate(ctx context.Context, tenantID string, id int64) (*VMTemplate, error) {
	tmpl, err := s.tmplDAO.GetByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrTemplateNotFound
		}
		return nil, err
	}
	return &tmpl, nil
}

func (s *templateService) ListTemplates(ctx context.Context, tenantID string, filter TemplateFilter) ([]VMTemplate, int64, error) {
	filter.TenantID = tenantID
	return s.tmplDAO.List(ctx, filter)
}

func (s *templateService) UpdateTemplate(ctx context.Context, tenantID string, id int64, req UpdateTemplateReq) error {
	// 检查模板是否存在
	_, err := s.tmplDAO.GetByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrTemplateNotFound
		}
		return err
	}

	// 如果更新名称，检查唯一性
	if req.Name != nil {
		existing, err := s.tmplDAO.GetByName(ctx, tenantID, *req.Name)
		if err == nil && existing.ID != id {
			return ErrTemplateNameExists
		}
		if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
			return err
		}
	}

	return s.tmplDAO.Update(ctx, tenantID, id, req)
}

func (s *templateService) DeleteTemplate(ctx context.Context, tenantID string, id int64) error {
	// 检查模板是否存在
	_, err := s.tmplDAO.GetByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrTemplateNotFound
		}
		return err
	}

	// 检查是否有进行中的任务
	count, err := s.taskDAO.CountRunningByTemplateID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrTemplateInUse
	}

	return s.tmplDAO.Delete(ctx, tenantID, id)
}

// ==================== 两种创建方式 ====================

func (s *templateService) ProvisionFromTemplate(ctx context.Context, tenantID, createdBy string, templateID int64, req ProvisionReq) (string, error) {
	if req.Count < 1 || req.Count > 20 {
		return "", ErrInvalidCount
	}

	// 获取模板
	tmpl, err := s.tmplDAO.GetByID(ctx, tenantID, templateID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return "", ErrTemplateNotFound
		}
		return "", err
	}

	// 确定实例名称前缀：覆盖参数优先，否则使用模板值
	namePrefix := tmpl.InstanceNamePrefix
	if req.InstanceNamePrefix != "" {
		namePrefix = req.InstanceNamePrefix
	}

	// 合并标签
	tags := make(map[string]string)
	for k, v := range tmpl.Tags {
		tags[k] = v
	}
	for k, v := range req.Tags {
		tags[k] = v
	}

	taskID := uuid.New().String()
	task := ProvisionTask{
		ID:                 taskID,
		TenantID:           tenantID,
		Source:             SourceFromTemplate,
		TemplateID:         templateID,
		Count:              req.Count,
		InstanceNamePrefix: namePrefix,
		OverrideTags:       req.Tags,
		Status:             TaskStatusPending,
		SyncStatus:         SyncStatusPending,
		CreatedBy:          createdBy,
	}

	if err := s.taskDAO.Insert(ctx, task); err != nil {
		return "", err
	}

	// 提交异步任务
	if s.submitter != nil {
		_ = s.submitter.Submit(ctx, "cam:create_ecs", map[string]interface{}{
			"task_id": taskID,
		})
	}

	return taskID, nil
}

func (s *templateService) DirectProvision(ctx context.Context, tenantID, createdBy string, req DirectProvisionReq) (string, error) {
	if req.Count < 1 || req.Count > 20 {
		return "", ErrInvalidCount
	}

	// 保存参数快照
	directParams := &DirectProvisionParams{
		Provider:           req.Provider,
		CloudAccountID:     req.CloudAccountID,
		Region:             req.Region,
		Zone:               req.Zone,
		InstanceType:       req.InstanceType,
		ImageID:            req.ImageID,
		VPCID:              req.VPCID,
		SubnetID:           req.SubnetID,
		SecurityGroupIDs:   req.SecurityGroupIDs,
		InstanceNamePrefix: req.InstanceNamePrefix,
		HostNamePrefix:     req.HostNamePrefix,
		SystemDiskType:     req.SystemDiskType,
		SystemDiskSize:     req.SystemDiskSize,
		DataDisks:          req.DataDisks,
		BandwidthOut:       req.BandwidthOut,
		ChargeType:         req.ChargeType,
		KeyPairName:        req.KeyPairName,
		Tags:               req.Tags,
		Description:        req.Description,
	}

	taskID := uuid.New().String()
	task := ProvisionTask{
		ID:                 taskID,
		TenantID:           tenantID,
		Source:             SourceDirect,
		TemplateID:         0,
		DirectParams:       directParams,
		Count:              req.Count,
		InstanceNamePrefix: req.InstanceNamePrefix,
		Status:             TaskStatusPending,
		SyncStatus:         SyncStatusPending,
		CreatedBy:          createdBy,
	}

	if err := s.taskDAO.Insert(ctx, task); err != nil {
		return "", err
	}

	// 提交异步任务
	if s.submitter != nil {
		_ = s.submitter.Submit(ctx, "cam:create_ecs", map[string]interface{}{
			"task_id": taskID,
		})
	}

	return taskID, nil
}

// ==================== 任务查询 ====================

func (s *templateService) ListProvisionTasks(ctx context.Context, tenantID string, filter ProvisionTaskFilter) ([]ProvisionTask, int64, error) {
	filter.TenantID = tenantID
	return s.taskDAO.List(ctx, filter)
}

func (s *templateService) GetProvisionTask(ctx context.Context, tenantID, taskID string) (*ProvisionTask, error) {
	task, err := s.taskDAO.GetByID(ctx, tenantID, taskID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errs.ErrorCode{Code: 404031, Msg: "provision task not found"}
		}
		return nil, err
	}
	return &task, nil
}

// ==================== 辅助方法 ====================

// BuildCreateInstanceParams 从模板构建统一创建参数
func BuildCreateInstanceParams(tmpl *VMTemplate, namePrefix string, tags map[string]string, count int) types.CreateInstanceParams {
	return types.CreateInstanceParams{
		Region:           tmpl.Region,
		Zone:             tmpl.Zone,
		InstanceType:     tmpl.InstanceType,
		ImageID:          tmpl.ImageID,
		VPCID:            tmpl.VPCID,
		SubnetID:         tmpl.SubnetID,
		SecurityGroupIDs: tmpl.SecurityGroupIDs,
		InstanceName:     namePrefix,
		HostName:         tmpl.HostNamePrefix,
		SystemDiskType:   tmpl.SystemDiskType,
		SystemDiskSize:   tmpl.SystemDiskSize,
		DataDisks:        convertDataDisks(tmpl.DataDisks),
		BandwidthOut:     tmpl.BandwidthOut,
		ChargeType:       tmpl.ChargeType,
		KeyPairName:      tmpl.KeyPairName,
		Tags:             tags,
		Count:            count,
	}
}

// BuildCreateInstanceParamsFromDirect 从直接创建请求构建统一创建参数
func BuildCreateInstanceParamsFromDirect(req *DirectProvisionReq) types.CreateInstanceParams {
	return types.CreateInstanceParams{
		Region:           req.Region,
		Zone:             req.Zone,
		InstanceType:     req.InstanceType,
		ImageID:          req.ImageID,
		VPCID:            req.VPCID,
		SubnetID:         req.SubnetID,
		SecurityGroupIDs: req.SecurityGroupIDs,
		InstanceName:     req.InstanceNamePrefix,
		HostName:         req.HostNamePrefix,
		SystemDiskType:   req.SystemDiskType,
		SystemDiskSize:   req.SystemDiskSize,
		DataDisks:        convertDataDisksFromConfig(req.DataDisks),
		BandwidthOut:     req.BandwidthOut,
		ChargeType:       req.ChargeType,
		KeyPairName:      req.KeyPairName,
		Tags:             req.Tags,
		Count:            req.Count,
	}
}

// GenerateInstanceName 生成带序号后缀的实例名称
func GenerateInstanceName(prefix string, index, total int) string {
	if total <= 1 {
		if prefix == "" {
			return "instance"
		}
		return prefix
	}
	if prefix == "" {
		prefix = "instance"
	}
	return fmt.Sprintf("%s-%03d", prefix, index+1)
}

func convertDataDisks(disks []DataDiskConfig) []types.DataDiskParam {
	if len(disks) == 0 {
		return nil
	}
	result := make([]types.DataDiskParam, len(disks))
	for i, d := range disks {
		result[i] = types.DataDiskParam{
			Category: d.Category,
			Size:     d.Size,
		}
	}
	return result
}

func convertDataDisksFromConfig(disks []DataDiskConfig) []types.DataDiskParam {
	return convertDataDisks(disks)
}

// Ensure compile-time interface compliance
var _ TemplateService = (*templateService)(nil)

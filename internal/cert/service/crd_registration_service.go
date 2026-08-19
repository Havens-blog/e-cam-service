package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/k8s"
)

// builtinOperator 内置默认登记的 operator 标记（系统播种，非人工登记）。
const builtinOperator = "system"

// CrdRegistrationView CRD 登记视图（Builtin 标记内置固定枚举项）。
type CrdRegistrationView struct {
	ID            string
	ClusterID     string
	APIGroup      string
	Kind          string
	CertFieldPath string
	Enabled       bool
	Operator      string
	Builtin       bool
	CreatedAt     time.Time
}

// RegisterCrdInput 自定义 CRD 登记入参（POST /settings/crds 载荷，4.5 落地）。
type RegisterCrdInput struct {
	ClusterID     string // 关联 cert_k8s_credentials 集群（= clusterName）
	APIGroup      string // 如 alb.alibabacloud.com；core 组资源为空串
	Kind          string // CRD kind
	CertFieldPath string // 证书引用字段路径，如 spec.certificates[].certificateId
	Operator      string
}

// CrdRegistrationService 自定义 CRD 扫描登记服务（任务 3.4）：登记/列表/删除/启停
// 与内置默认登记初始化。clusterId+apiGroup+kind 唯一冲突返回
// domain.ErrDuplicateCrdRegistration（供 4.5 映射 409）；certFieldPath 语法
// 在校验期拒绝（k8s.ErrInvalidCertFieldPath，含可读错误信息）。
type CrdRegistrationService interface {
	// Register 登记自定义 CRD：certFieldPath 语法校验 → 唯一冲突哨兵；
	// 命中内置固定枚举（apiGroup+kind）时拒绝（内置项由 EnsureBuiltinRegistrations
	// 播种，语义冲突）。登记成功默认 enabled=true（随扫描范围生效）。
	Register(ctx context.Context, in RegisterCrdInput) (CrdRegistrationView, error)
	// List 全量登记（Builtin 标记内置项）。
	List(ctx context.Context) ([]CrdRegistrationView, error)
	// SetEnabled 启停登记（enabled=false 时该 CRD 回归盲区，视图显式声明）。
	SetEnabled(ctx context.Context, id string, enabled bool) error
	// Delete 删除登记：内置固定枚举项不可删除（domain.ErrBuiltinCrdRegistration，
	// 明确错误），自定义登记正常删除。
	Delete(ctx context.Context, id string) error
	// EnsureBuiltinRegistrations 为指定集群幂等初始化四类内置默认登记
	//（ALBConfig/Ingress/Gateway/HTTPRoute，enabled=true）；已存在项跳过
	//（不覆盖 enabled 等人工状态），可安全重试。
	EnsureBuiltinRegistrations(ctx context.Context, clusterID string) error
}

type crdRegistrationService struct {
	regs domain.CrdRegistrationRepository
}

// NewCrdRegistrationService 创建 CRD 登记服务。
func NewCrdRegistrationService(regs domain.CrdRegistrationRepository) CrdRegistrationService {
	return &crdRegistrationService{regs: regs}
}

// Register 登记自定义 CRD（certFieldPath 校验期拒绝非法路径）。
func (s *crdRegistrationService) Register(ctx context.Context, in RegisterCrdInput) (CrdRegistrationView, error) {
	clusterID := strings.TrimSpace(in.ClusterID)
	kind := strings.TrimSpace(in.Kind)
	apiGroup := strings.TrimSpace(in.APIGroup)
	if clusterID == "" {
		return CrdRegistrationView{}, fmt.Errorf("cert: clusterId is required")
	}
	if kind == "" {
		return CrdRegistrationView{}, fmt.Errorf("cert: kind is required")
	}
	// 校验期拒绝非法 certFieldPath（AC：不可解析路径拒绝 + 可读错误信息）
	if err := k8s.ValidateCertFieldPath(in.CertFieldPath); err != nil {
		return CrdRegistrationView{}, err
	}
	// 内置固定枚举项由播种初始化：同 apiGroup+kind 的人工登记属语义冲突
	if k8s.IsBuiltinRegistration(apiGroup, kind) {
		return CrdRegistrationView{}, fmt.Errorf("cert: %s/%s is a builtin crd registration and cannot be re-registered", apiGroup, kind)
	}
	reg := &domain.CrdRegistration{
		ClusterID:     clusterID,
		APIGroup:      apiGroup,
		Kind:          kind,
		CertFieldPath: in.CertFieldPath,
		Operator:      strings.TrimSpace(in.Operator),
	}
	if err := s.regs.Create(ctx, reg); err != nil {
		return CrdRegistrationView{}, err // uk_cluster_group_kind 冲突 → ErrDuplicateCrdRegistration
	}
	return toCrdRegistrationView(*reg), nil
}

// List 全量登记（含 Builtin 标记）。
func (s *crdRegistrationService) List(ctx context.Context) ([]CrdRegistrationView, error) {
	regs, err := s.regs.List(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]CrdRegistrationView, 0, len(regs))
	for _, r := range regs {
		views = append(views, toCrdRegistrationView(r))
	}
	return views, nil
}

// SetEnabled 启停登记（内置项同样允许停用：停用后该 CRD 回归盲区并显式声明）。
func (s *crdRegistrationService) SetEnabled(ctx context.Context, id string, enabled bool) error {
	if _, err := s.regs.GetByID(ctx, id); err != nil {
		return err // ErrInvalidID / mongo.ErrNoDocuments 透传
	}
	return s.regs.SetEnabled(ctx, id, enabled)
}

// Delete 删除登记；内置固定枚举项不可删除（明确错误）。
func (s *crdRegistrationService) Delete(ctx context.Context, id string) error {
	reg, err := s.regs.GetByID(ctx, id)
	if err != nil {
		return err // ErrInvalidID / mongo.ErrNoDocuments 透传
	}
	if k8s.IsBuiltinRegistration(reg.APIGroup, reg.Kind) {
		return fmt.Errorf("%w: %s/%s is a builtin registration (disable via SetEnabled instead)",
			domain.ErrBuiltinCrdRegistration, reg.APIGroup, reg.Kind)
	}
	return s.regs.DeleteByID(ctx, id)
}

// EnsureBuiltinRegistrations 幂等初始化四类内置默认登记（enabled=true，
// 随扫描范围生效）：逐项 Create，唯一冲突视为已存在跳过；不覆盖既有项的
// enabled 状态（保留人工停用决策）。
func (s *crdRegistrationService) EnsureBuiltinRegistrations(ctx context.Context, clusterID string) error {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" {
		return fmt.Errorf("cert: clusterId is required")
	}
	for _, b := range k8s.BuiltinRegistrations {
		reg := &domain.CrdRegistration{
			ClusterID:     clusterID,
			APIGroup:      b.APIGroup,
			Kind:          b.Kind,
			CertFieldPath: b.CertFieldPath,
			Operator:      builtinOperator,
		}
		err := s.regs.Create(ctx, reg)
		switch {
		case err == nil, errors.Is(err, domain.ErrDuplicateCrdRegistration):
			continue // 已存在跳过（幂等）
		default:
			return err
		}
	}
	return nil
}

// toCrdRegistrationView 登记文档 → 视图（Builtin 标记）。
func toCrdRegistrationView(r domain.CrdRegistration) CrdRegistrationView {
	return CrdRegistrationView{
		ID:            r.ID.Hex(),
		ClusterID:     r.ClusterID,
		APIGroup:      r.APIGroup,
		Kind:          r.Kind,
		CertFieldPath: r.CertFieldPath,
		Enabled:       r.Enabled,
		Operator:      r.Operator,
		Builtin:       k8s.IsBuiltinRegistration(r.APIGroup, r.Kind),
		CreatedAt:     r.CreatedAt,
	}
}

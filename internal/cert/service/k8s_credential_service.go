package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/k8s"
)

// BuiltinSeeder 内置默认登记播种接口（由 CrdRegistrationService 实现）：
// AddCluster 成功后为该集群幂等初始化四类固定枚举登记（ALBConfig/Ingress/
// Gateway/HTTPRoute），使其随扫描范围生效。
type BuiltinSeeder interface {
	EnsureBuiltinRegistrations(ctx context.Context, clusterID string) error
}

// ClientCacheInvalidator dynamic client 工厂缓存失效接口（由 k8s.Factory 实现）：
// 删除集群凭证后失效其缓存 client，防止已删除集群的连接被复用。
type ClientCacheInvalidator interface {
	Invalidate(clusterName string)
}

// K8sCredentialView 集群凭证视图（Hard Rule 白名单：仅 clusterName/apiEndpoint/
// createdAt 三字段；任何读取路径不返回 kubeconfig 明文或密文）。
type K8sCredentialView struct {
	ClusterName string
	APIEndpoint string
	CreatedAt   time.Time
}

// AddK8sCredentialInput 新增集群凭证入参。
// Kubeconfig 为明文 kubeconfig（YAML）：仅内存用于校验与加密，落库前即转密文；
// 输入缓冲归调用方所有（web 层负责用后清零，与导入服务入参约定一致）。
type AddK8sCredentialInput struct {
	ClusterName string
	Kubeconfig  []byte
	APIEndpoint string
}

// K8sCredentialService K8s 集群凭证服务（任务 3.4）：新增/列表/删除集群凭证，
// kubeconfig 经 1.1 信封加密（AES-256-GCM + keyVersion）落库。
// HTTP 端点在 4.5 落地。
type K8sCredentialService interface {
	// AddCluster 登记集群凭证：kubeconfig 可解析性校验 → 信封加密落库（clusterName
	// 唯一，冲突返回 domain.ErrDuplicateClusterName）→ 播种内置默认登记（幂等）。
	AddCluster(ctx context.Context, in AddK8sCredentialInput) (K8sCredentialView, error)
	// ListClusters 全量集群凭证（白名单视图，不含 kubeconfig 任何形态）。
	ListClusters(ctx context.Context) ([]K8sCredentialView, error)
	// DeleteCluster 按集群名删除凭证并失效 dynamic client 缓存；
	// 未命中返回 mongo.ErrNoDocuments。
	DeleteCluster(ctx context.Context, clusterName string) error
}

type k8sCredentialService struct {
	creds  domain.K8sCredentialRepository
	crypto *domain.EnvelopeCrypto
	seeder BuiltinSeeder          // 可为 nil（未装配播种时不阻塞凭证登记）
	cache  ClientCacheInvalidator // 可为 nil
}

// NewK8sCredentialService 创建集群凭证服务；seeder/cache 为可选依赖（nil 安全）。
func NewK8sCredentialService(
	creds domain.K8sCredentialRepository,
	crypto *domain.EnvelopeCrypto,
	seeder BuiltinSeeder,
	cache ClientCacheInvalidator,
) K8sCredentialService {
	return &k8sCredentialService{creds: creds, crypto: crypto, seeder: seeder, cache: cache}
}

// AddCluster 登记集群凭证。播种内置登记失败时凭证行已落库——播种幂等，
// 可安全重试（EnsureBuiltinRegistrations 对已存在项跳过）。
func (s *k8sCredentialService) AddCluster(ctx context.Context, in AddK8sCredentialInput) (K8sCredentialView, error) {
	name := strings.TrimSpace(in.ClusterName)
	if name == "" {
		return K8sCredentialView{}, fmt.Errorf("cert: clusterName is required")
	}
	// 可解析性快速失败：避免密文落库后才发现凭证不可用（K8S_UNREACHABLE 延迟暴露）
	if err := k8s.ValidateKubeconfig(in.Kubeconfig); err != nil {
		return K8sCredentialView{}, err
	}
	if s.crypto == nil {
		return K8sCredentialView{}, errCryptoMissing
	}
	ciphertext, keyVersion, err := s.crypto.Encrypt(in.Kubeconfig)
	if err != nil {
		return K8sCredentialView{}, fmt.Errorf("cert: encrypt kubeconfig for cluster %q: %w", name, err)
	}
	cred := &domain.K8sCredential{
		ClusterName: name,
		Kubeconfig: &domain.EncryptedSecret{
			Ciphertext: ciphertext,
			KeyVersion: keyVersion,
			Algo:       domain.AlgoAES256GCM,
		},
		APIEndpoint: strings.TrimSpace(in.APIEndpoint),
	}
	if err := s.creds.Create(ctx, cred); err != nil {
		return K8sCredentialView{}, err // uk_cluster_name 冲突 → ErrDuplicateClusterName
	}
	if s.seeder != nil {
		if err := s.seeder.EnsureBuiltinRegistrations(ctx, name); err != nil {
			return K8sCredentialView{}, fmt.Errorf("cert: cluster %q registered but builtin crd registration init failed (retryable): %w", name, err)
		}
	}
	return K8sCredentialView{ClusterName: cred.ClusterName, APIEndpoint: cred.APIEndpoint, CreatedAt: cred.CreatedAt}, nil
}

// ListClusters 白名单视图（永不携带 kubeconfig 明文/密文）。
func (s *k8sCredentialService) ListClusters(ctx context.Context) ([]K8sCredentialView, error) {
	creds, err := s.creds.List(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]K8sCredentialView, 0, len(creds))
	for _, c := range creds {
		views = append(views, K8sCredentialView{
			ClusterName: c.ClusterName,
			APIEndpoint: c.APIEndpoint,
			CreatedAt:   c.CreatedAt,
		})
	}
	return views, nil
}

// DeleteCluster 删除凭证并失效 dynamic client 缓存。
func (s *k8sCredentialService) DeleteCluster(ctx context.Context, clusterName string) error {
	name := strings.TrimSpace(clusterName)
	if name == "" {
		return fmt.Errorf("cert: clusterName is required")
	}
	if _, err := s.creds.GetByClusterName(ctx, name); err != nil {
		return err // ErrNoDocuments 透传（404 语义）
	}
	if err := s.creds.DeleteByClusterName(ctx, name); err != nil {
		return err
	}
	if s.cache != nil {
		s.cache.Invalidate(name)
	}
	return nil
}

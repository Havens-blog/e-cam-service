package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
)

// K8sCredentialsCollection K8s 集群接入凭证集合名。
const K8sCredentialsCollection = "cert_k8s_credentials"

type k8sCredentialRepository struct {
	db *mongox.Mongo
}

// NewK8sCredentialRepository 创建 K8s 集群凭证仓储。
// 硬约束：本仓储不提供任何返回 kubeconfig 明文的方法——解密仅发生在 service 层内存中
// （domain.EnvelopeCrypto.Decrypt + defer domain.Zeroize）。
func NewK8sCredentialRepository(db *mongox.Mongo) domain.K8sCredentialRepository {
	return &k8sCredentialRepository{db: db}
}

// Create 写入集群凭证；clusterName 冲突（uk_cluster_name）返回 ErrDuplicateClusterName；
// DEFAULT 填充：createdAt=now。
func (r *k8sCredentialRepository) Create(ctx context.Context, c *domain.K8sCredential) error {
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	_, err := r.db.Collection(K8sCredentialsCollection).InsertOne(ctx, c)
	return mapDupKey(err, domain.ErrDuplicateClusterName)
}

// GetByClusterName 按集群名查询；未命中返回 mongo.ErrNoDocuments。
func (r *k8sCredentialRepository) GetByClusterName(ctx context.Context, clusterName string) (domain.K8sCredential, error) {
	var cred domain.K8sCredential
	err := r.db.Collection(K8sCredentialsCollection).
		FindOne(ctx, bson.M{"clusterName": clusterName}).Decode(&cred)
	return cred, err
}

// List 全量集群凭证（扫描范围联动）。
func (r *k8sCredentialRepository) List(ctx context.Context) ([]domain.K8sCredential, error) {
	cursor, err := r.db.Collection(K8sCredentialsCollection).Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var creds []domain.K8sCredential
	if err := cursor.All(ctx, &creds); err != nil {
		return nil, err
	}
	return creds, nil
}

// DeleteByClusterName 按集群名删除。
func (r *k8sCredentialRepository) DeleteByClusterName(ctx context.Context, clusterName string) error {
	_, err := r.db.Collection(K8sCredentialsCollection).DeleteOne(ctx, bson.M{"clusterName": clusterName})
	return err
}

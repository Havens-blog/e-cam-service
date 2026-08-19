package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// K8sCredential K8s 集群接入凭证文档（cert_k8s_credentials，clusterName 唯一 uk_cluster_name）。
// kubeconfig 加密存储（同私钥信封加密体系）；明文仅内存解密用后 Zeroize。
type K8sCredential struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	ClusterName string             `bson:"clusterName"` // 唯一
	Kubeconfig  *EncryptedSecret   `bson:"kubeconfig"`  // 密文形态，必填
	APIEndpoint string             `bson:"apiEndpoint,omitempty"`
	CreatedAt   time.Time          `bson:"createdAt"` // DEFAULT=now()
}

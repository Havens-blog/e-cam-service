package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// CrdRegistration 自定义 CRD 扫描登记文档（cert_crd_registrations）。
// uk_cluster_group_kind（clusterId+apiGroup+kind）登记去重；
// K8sAPIChannel 扫描范围 = 固定枚举（ALBConfig/Ingress/Gateway/HTTPRoute）+ enabled=true 登记项。
type CrdRegistration struct {
	ID            primitive.ObjectID `bson:"_id,omitempty"`
	ClusterID     string             `bson:"clusterId"`     // 关联 k8s_credentials 集群；登记限定单集群
	APIGroup      string             `bson:"apiGroup"`      // 如 alb.alibabacloud.com；core 组资源为空串
	Kind          string             `bson:"kind"`          // CRD kind
	CertFieldPath string             `bson:"certFieldPath"` // 云托管证书引用字段路径，如 spec.certificates[].certificateId
	Enabled       bool               `bson:"enabled"`       // DEFAULT=true；false=停用登记（该 CRD 回归盲区）
	Operator      string             `bson:"operator"`
	CreatedAt     time.Time          `bson:"createdAt"` // DEFAULT=now()
}

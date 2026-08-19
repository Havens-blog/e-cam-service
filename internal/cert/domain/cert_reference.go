package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Cloud 云厂商枚举。对齐 schema.sql cert_references.cloud enum。
type Cloud string

const (
	CloudAliyun  Cloud = "aliyun"
	CloudTencent Cloud = "tencent"
	CloudHuawei  Cloud = "huawei"
	CloudAWS     Cloud = "aws"
	CloudAzure   Cloud = "azure"
)

// Product 云产品枚举。对齐 schema.sql cert_references.product enum。
// crd 仅出现在引用发现（cert_references），可部署产品为 cdn/dcdn/waf/alb/clb/nlb
// （见 schema.sql cert_change_items.resourceRef.product enum）。
type Product string

const (
	ProductCDN  Product = "cdn"
	ProductDCDN Product = "dcdn"
	ProductWAF  Product = "waf"
	ProductALB  Product = "alb"
	ProductCLB  Product = "clb"
	ProductNLB  Product = "nlb"
	ProductCRD  Product = "crd"
)

// CertReference 引用扫描发现文档（cert_references）。
// 关联 certificates.fingerprint 为应用层引用（非 FK 约束）。
type CertReference struct {
	ID                    primitive.ObjectID `bson:"_id,omitempty"`
	CertFingerprint       string             `bson:"certFingerprint"` // ^[0-9a-f]{64}$（无法解析时为确定性占位指纹，见 service 层）
	Cloud                 Cloud              `bson:"cloud,omitempty"`
	Product               Product            `bson:"product,omitempty"`
	ClusterID             string             `bson:"clusterId,omitempty"`             // K8s 集群（product=crd 时必填）
	Namespace             string             `bson:"namespace,omitempty"`             // K8s 命名空间（product=crd 时写通，任务 3.5）
	Kind                  string             `bson:"kind,omitempty"`                  // CRD kind（product=crd 时写通，任务 3.5；5.x 变更项重构 DeployTarget 依据）
	ResourceID            string             `bson:"resourceId,omitempty"`            // 云资源 ID / CRD 实例名
	ReferencedCloudCertID string             `bson:"referencedCloudCertId,omitempty"` // 云侧证书 ID
	AccountKey            string             `bson:"accountKey,omitempty"`
	SnapshotID            string             `bson:"snapshotId"` // 来源扫描快照
	ScannedAt             time.Time          `bson:"scannedAt"`  // DEFAULT=now()
}

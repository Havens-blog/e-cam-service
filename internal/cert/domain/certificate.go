package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// HostingStatus 证书托管状态。
// 对齐 schema.sql cert_certificates.hostingStatus enum。
type HostingStatus string

const (
	// HostingStatusComplete 完整托管（含信封加密私钥，可上传云证书库执行更换）。
	HostingStatusComplete HostingStatus = "complete"
	// HostingStatusFingerprintOnly 仅指纹登记（无私钥；进入变更清单时为不可执行项）。
	HostingStatusFingerprintOnly HostingStatus = "fingerprint_only"
)

// KeyAlgorithm 证书公钥算法。
// 对齐 schema.sql cert_certificates.keyAlgorithm enum。
type KeyAlgorithm string

const (
	KeyAlgorithmRSA   KeyAlgorithm = "RSA"
	KeyAlgorithmECDSA KeyAlgorithm = "ECDSA"
)

// ExpiryAlertLevel 到期分级告警去重状态（持久化已触发级别）。
// 对齐 schema.sql cert_certificates.expiryAlertLevel enum；
// 仅当新级别较已触发级别更紧急（升级）才发送告警并更新，同级不重复触发。
type ExpiryAlertLevel string

const (
	ExpiryAlertNone    ExpiryAlertLevel = "none"
	ExpiryAlertL30     ExpiryAlertLevel = "L30"
	ExpiryAlertL14     ExpiryAlertLevel = "L14"
	ExpiryAlertL7      ExpiryAlertLevel = "L7"
	ExpiryAlertExpired ExpiryAlertLevel = "expired"
)

// EncryptedSecret 信封加密密文形态，encryptedPrivateKey 与 kubeconfig 共用。
// 字段形状对齐 schema.sql：{ciphertext, keyVersion, algo}。
//
// 硬约束：明文永不进入本模型与仓储层——解密仅发生在 service 层内存中
// （EnvelopeCrypto.Decrypt + defer Zeroize），仓储层不提供任何返回明文的方法。
type EncryptedSecret struct {
	Ciphertext string `bson:"ciphertext"` // AES-256-GCM 密文(base64，nonce 前置)
	KeyVersion int    `bson:"keyVersion"` // 主密钥版本（>=1，双读解密路由依据）
	Algo       string `bson:"algo"`       // 恒为 AlgoAES256GCM
}

// MaterialIssue 盘点登记的材料异常标记（发现导入容忍模式写入；空=正常）。
// 口径：expired 优先于 chain_incomplete（同时命中记运营主导事实）；
// materialIssue=expired 的证书不参与到期告警（已知存量，处置动作是换证）。
type MaterialIssue string

const (
	// MaterialIssueExpired 已过期/未生效（发现导入容忍登记）。
	MaterialIssueExpired MaterialIssue = "expired"
	// MaterialIssueChainIncomplete 证书链缺自签根/链不可验证（发现导入容忍登记）。
	MaterialIssueChainIncomplete MaterialIssue = "chain_incomplete"
)

// Certificate 证书台账文档（cert_certificates）。
// 字段名/bson tag 与 schema.sql 1:1 对齐；fingerprint 唯一（uk_fingerprint）。
type Certificate struct {
	ID                  primitive.ObjectID `bson:"_id,omitempty"`
	Fingerprint         string             `bson:"fingerprint"` // SHA256 hex，聚合主键
	CommonName          string             `bson:"commonName,omitempty"`
	Sans                []string           `bson:"sans,omitempty"` // SAN 域名数组
	Issuer              string             `bson:"issuer,omitempty"`
	SerialNumber        string             `bson:"serialNumber,omitempty"`
	NotBefore           time.Time          `bson:"notBefore,omitempty"`
	NotAfter            time.Time          `bson:"notAfter,omitempty"`
	KeyAlgorithm        KeyAlgorithm       `bson:"keyAlgorithm,omitempty"`
	HostingStatus       HostingStatus      `bson:"hostingStatus"`
	MaterialIssue       MaterialIssue      `bson:"materialIssue,omitempty"` // 盘点登记材料异常标记（发现导入容忍模式；空=正常）
	EncryptedPrivateKey *EncryptedSecret   `bson:"encryptedPrivateKey,omitempty"` // 仅指纹登记时缺省；永不出现在 API 响应
	CertPEM             string             `bson:"certPem,omitempty"`             // 导入时上传的证书束 PEM 原文（leaf+中间链+根）；补传私钥匹配校验与云证书库上传依据；永不出现在 API 响应
	ExpectedDomain      string             `bson:"expectedDomain,omitempty"`      // 可选，仅提示性比对
	ProtectUntil        *time.Time         `bson:"protectUntil,omitempty"`        // 回滚保护期截止；>=now 禁删
	ExpiryAlertLevel    ExpiryAlertLevel   `bson:"expiryAlertLevel,omitempty"`    // DEFAULT="none"
	CreatedAt           time.Time          `bson:"createdAt"`                     // DEFAULT=now()
}

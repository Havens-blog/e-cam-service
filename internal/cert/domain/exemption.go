package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Exemption 探测豁免清单文档（cert_exemptions，domain 唯一 uk_domain）。
// 豁免域名仍探测但标 exempt（不告警）；验证窗口构建 verifyExpected.domains 时剔除并记入 excludedDomains。
type Exemption struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	Domain    string             `bson:"domain"` // 唯一
	Reason    string             `bson:"reason,omitempty"`
	Operator  string             `bson:"operator,omitempty"`
	CreatedAt time.Time          `bson:"createdAt"` // DEFAULT=now()
}

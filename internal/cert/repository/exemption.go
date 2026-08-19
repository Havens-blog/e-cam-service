package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
)

// ExemptionsCollection 探测豁免清单集合名。
const ExemptionsCollection = "cert_exemptions"

type exemptionRepository struct {
	db *mongox.Mongo
}

// NewExemptionRepository 创建豁免清单仓储。
func NewExemptionRepository(db *mongox.Mongo) domain.ExemptionRepository {
	return &exemptionRepository{db: db}
}

// Upsert 按唯一 domain 写入；DEFAULT 填充：createdAt=now。
func (r *exemptionRepository) Upsert(ctx context.Context, e *domain.Exemption) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	_, err := r.db.Collection(ExemptionsCollection).UpdateOne(ctx,
		bson.M{"domain": e.Domain},
		bson.M{"$set": e},
		optionsUpsert())
	return err
}

// List 全量豁免清单（探测目标构建与验证窗口剔除依据）。
func (r *exemptionRepository) List(ctx context.Context) ([]domain.Exemption, error) {
	cursor, err := r.db.Collection(ExemptionsCollection).Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var exemptions []domain.Exemption
	if err := cursor.All(ctx, &exemptions); err != nil {
		return nil, err
	}
	return exemptions, nil
}

// DeleteByDomain 按域名删除豁免。
func (r *exemptionRepository) DeleteByDomain(ctx context.Context, domainName string) error {
	_, err := r.db.Collection(ExemptionsCollection).DeleteOne(ctx, bson.M{"domain": domainName})
	return err
}

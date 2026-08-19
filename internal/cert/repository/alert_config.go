package repository

import (
	"context"
	"errors"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// AlertConfigCollection 全局告警配置集合名（单文档，固定 _id="global"）。
const AlertConfigCollection = "cert_alert_config"

type alertConfigRepository struct {
	db *mongox.Mongo
}

// NewAlertConfigRepository 创建全局告警配置仓储。
func NewAlertConfigRepository(db *mongox.Mongo) domain.AlertConfigRepository {
	return &alertConfigRepository{db: db}
}

// Get 读取全局配置；文档不存在时返回 schema.sql DEFAULT 填充的默认配置
// （MongoDB 无列级 DEFAULT，默认值由 repository 写入路径保证）。
func (r *alertConfigRepository) Get(ctx context.Context) (domain.AlertConfig, error) {
	var cfg domain.AlertConfig
	err := r.db.Collection(AlertConfigCollection).
		FindOne(ctx, bson.M{"_id": domain.AlertConfigID}).Decode(&cfg)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return domain.DefaultAlertConfig(), nil
	}
	if err != nil {
		return domain.AlertConfig{}, err
	}
	return cfg, nil
}

// Save 以 _id="global" upsert 保存（调用方传入的 ID 被强制覆盖为固定值）。
func (r *alertConfigRepository) Save(ctx context.Context, cfg *domain.AlertConfig) error {
	cfg.ID = domain.AlertConfigID
	if len(cfg.WebhookURLs) == 0 {
		cfg.WebhookURLs = []string{}
	}
	if len(cfg.EmailGroup) == 0 {
		cfg.EmailGroup = []string{}
	}
	if len(cfg.Thresholds.ExpiryLevels) == 0 {
		cfg.Thresholds = domain.DefaultThresholds()
	}
	_, err := r.db.Collection(AlertConfigCollection).UpdateOne(ctx,
		bson.M{"_id": domain.AlertConfigID},
		bson.M{"$set": cfg},
		optionsUpsert())
	return err
}

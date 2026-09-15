package dao

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const CDNMetricCollection = "ecam_cdn_metric"

// CDNMetricDAO CDN 单日指标数据访问接口
type CDNMetricDAO interface {
	// UpsertMetric 按 (domain, date) 幂等写入单日指标,同日重采覆盖为最新值
	UpsertMetric(ctx context.Context, m types.CDNMetric) error
}

type cdnMetricDAO struct {
	db *mongox.Mongo
}

// NewCDNMetricDAO 创建 CDN 指标 DAO,并确保 (domain, date) 唯一索引
func NewCDNMetricDAO(db *mongox.Mongo) CDNMetricDAO {
	d := &cdnMetricDAO{db: db}
	_, _ = db.Collection(CDNMetricCollection).Indexes().CreateOne(
		context.Background(), mongo.IndexModel{
			Keys: bson.D{
				{Key: "domain", Value: 1},
				{Key: "date", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		})
	return d
}

func (d *cdnMetricDAO) UpsertMetric(ctx context.Context, m types.CDNMetric) error {
	filter := bson.M{
		"domain": m.Domain,
		"date":   m.Date,
	}
	update := bson.M{
		"$set": bson.M{
			"bytes":      m.Bytes,
			"bandwidth":  m.Bandwidth,
			"hit_rate":   m.HitRate,
			"account_id": m.AccountID,
			"provider":   m.Provider,
		},
		"$setOnInsert": bson.M{
			"domain": m.Domain,
			"date":   m.Date,
		},
	}
	_, err := d.db.Collection(CDNMetricCollection).UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

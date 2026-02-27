package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// DashboardDAO 仪表盘数据访问接口
type DashboardDAO interface {
	// CountByProvider 按云厂商统计资产数量
	CountByProvider(ctx context.Context, tenantID string) ([]GroupCount, error)
	// CountByAssetType 按资产类型统计数量
	CountByAssetType(ctx context.Context, tenantID string) ([]GroupCount, error)
	// CountByRegion 按地域统计资产数量
	CountByRegion(ctx context.Context, tenantID string) ([]GroupCount, error)
	// CountByAccountID 按云账号统计资产数量
	CountByAccountID(ctx context.Context, tenantID string) ([]GroupCount, error)
	// GetExpiringInstances 获取即将过期的资源列表
	GetExpiringInstances(ctx context.Context, tenantID string, withinDays int, offset, limit int64) ([]Instance, int64, error)
	// GetTotalCount 获取资产总数
	GetTotalCount(ctx context.Context, tenantID string) (int64, error)
	// CountByStatus 按状态统计资产数量
	CountByStatus(ctx context.Context, tenantID string) ([]GroupCount, error)
}

// GroupCount 分组统计结果
type GroupCount struct {
	Key   string `bson:"_id" json:"key"`
	Count int64  `bson:"count" json:"count"`
}

type dashboardDAO struct {
	db *mongox.Mongo
}

// NewDashboardDAO 创建仪表盘DAO
func NewDashboardDAO(db *mongox.Mongo) DashboardDAO {
	return &dashboardDAO{db: db}
}

func (d *dashboardDAO) collection() *mongo.Collection {
	return d.db.Collection(InstanceCollection)
}

// CountByProvider 按云厂商统计
func (d *dashboardDAO) CountByProvider(ctx context.Context, tenantID string) ([]GroupCount, error) {
	return d.aggregateGroup(ctx, tenantID, "$attributes.provider")
}

// CountByAssetType 按资产类型统计
func (d *dashboardDAO) CountByAssetType(ctx context.Context, tenantID string) ([]GroupCount, error) {
	return d.aggregateGroup(ctx, tenantID, "$model_uid")
}

// CountByRegion 按地域统计
func (d *dashboardDAO) CountByRegion(ctx context.Context, tenantID string) ([]GroupCount, error) {
	return d.aggregateGroup(ctx, tenantID, "$attributes.region")
}

// CountByAccountID 按云账号统计
func (d *dashboardDAO) CountByAccountID(ctx context.Context, tenantID string) ([]GroupCount, error) {
	return d.aggregateGroup(ctx, tenantID, "$account_id")
}

// CountByStatus 按状态统计
func (d *dashboardDAO) CountByStatus(ctx context.Context, tenantID string) ([]GroupCount, error) {
	return d.aggregateGroup(ctx, tenantID, "$attributes.status")
}

// GetTotalCount 获取资产总数
func (d *dashboardDAO) GetTotalCount(ctx context.Context, tenantID string) (int64, error) {
	filter := bson.M{}
	if tenantID != "" {
		filter["tenant_id"] = tenantID
	}
	return d.collection().CountDocuments(ctx, filter)
}

// GetExpiringInstances 获取即将过期的资源
func (d *dashboardDAO) GetExpiringInstances(ctx context.Context, tenantID string, withinDays int, offset, limit int64) ([]Instance, int64, error) {
	now := time.Now()
	deadline := now.AddDate(0, 0, withinDays)

	// 过期时间存储在 attributes.expire_time，格式为 RFC3339 字符串
	// 查询条件: expire_time 存在 且 expire_time <= deadline 且 expire_time > now
	filter := bson.M{
		"attributes.expire_time": bson.M{
			"$exists": true,
			"$ne":     "",
			"$gt":     now.Format(time.RFC3339),
			"$lte":    deadline.Format(time.RFC3339),
		},
	}
	if tenantID != "" {
		filter["tenant_id"] = tenantID
	}

	total, err := d.collection().CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "attributes.expire_time", Value: 1}}).
		SetSkip(offset).
		SetLimit(limit)

	cursor, err := d.collection().Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var instances []Instance
	if err := cursor.All(ctx, &instances); err != nil {
		return nil, 0, err
	}

	return instances, total, nil
}

// aggregateGroup 通用分组聚合
func (d *dashboardDAO) aggregateGroup(ctx context.Context, tenantID, groupField string) ([]GroupCount, error) {
	pipeline := mongo.Pipeline{}

	// match 阶段
	match := bson.M{}
	if tenantID != "" {
		match["tenant_id"] = tenantID
	}
	if len(match) > 0 {
		pipeline = append(pipeline, bson.D{{Key: "$match", Value: match}})
	}

	// group 阶段
	pipeline = append(pipeline, bson.D{
		{Key: "$group", Value: bson.D{
			{Key: "_id", Value: groupField},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}},
	})

	// sort 阶段 (按数量降序)
	pipeline = append(pipeline, bson.D{
		{Key: "$sort", Value: bson.D{{Key: "count", Value: -1}}},
	})

	cursor, err := d.collection().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []GroupCount
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	return results, nil
}

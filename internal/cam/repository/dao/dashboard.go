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
	CountByProvider(ctx context.Context, tenantID int64) ([]GroupCount, error)
	// CountByAssetType 按资产类型统计数量
	CountByAssetType(ctx context.Context, tenantID int64) ([]GroupCount, error)
	// CountByRegion 按地域统计资产数量
	CountByRegion(ctx context.Context, tenantID int64) ([]GroupCount, error)
	// CountByAccountID 按云账号统计资产数量
	CountByAccountID(ctx context.Context, tenantID int64) ([]GroupCount, error)
	// GetExpiringInstances 获取即将过期的资源列表
	GetExpiringInstances(ctx context.Context, tenantID int64, withinDays int, offset, limit int64) ([]Instance, int64, error)
	// GetTotalCount 获取资产总数
	GetTotalCount(ctx context.Context, tenantID int64) (int64, error)
	// CountByStatus 按状态统计资产数量
	CountByStatus(ctx context.Context, tenantID int64) ([]GroupCount, error)
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
func (d *dashboardDAO) CountByProvider(ctx context.Context, tenantID int64) ([]GroupCount, error) {
	return d.aggregateGroup(ctx, tenantID, "$attributes.provider")
}

// CountByAssetType 按资产类型统计
func (d *dashboardDAO) CountByAssetType(ctx context.Context, tenantID int64) ([]GroupCount, error) {
	return d.aggregateGroup(ctx, tenantID, "$model_uid")
}

// CountByRegion 按地域统计
func (d *dashboardDAO) CountByRegion(ctx context.Context, tenantID int64) ([]GroupCount, error) {
	return d.aggregateGroup(ctx, tenantID, "$attributes.region")
}

// CountByAccountID 按云账号统计
func (d *dashboardDAO) CountByAccountID(ctx context.Context, tenantID int64) ([]GroupCount, error) {
	return d.aggregateGroup(ctx, tenantID, "$account_id")
}

// CountByStatus 按状态统计
func (d *dashboardDAO) CountByStatus(ctx context.Context, tenantID int64) ([]GroupCount, error) {
	return d.aggregateGroup(ctx, tenantID, "$attributes.status")
}

// GetTotalCount 获取资产总数
func (d *dashboardDAO) GetTotalCount(ctx context.Context, tenantID int64) (int64, error) {
	filter := bson.M{}
	// 租户过滤恒定生效：0 不作通配。未选定租户时本查询返回空集，
	// 而不是退化为「不加过滤」从而返回全部租户数据。
	filter["tenant_id"] = tenantID
	return d.collection().CountDocuments(ctx, filter)
}

// GetExpiringInstances 获取即将过期的资源
func (d *dashboardDAO) GetExpiringInstances(ctx context.Context, tenantID int64, withinDays int, offset, limit int64) ([]Instance, int64, error) {
	now := time.Now()
	deadline := now.AddDate(0, 0, withinDays)

	// 到期时间落在 attributes.expired_time（执行器同步路径写入的字段名），
	// 云厂商格式混存多种字符串与 BSON date（同 timex.ParseExpiredTime 的输入）。
	// 在服务端归一化为 BSON date 后过滤/排序/分页，仅传输当前页的瘦身文档——
	// 曾因全量拉取肥文档到内存过滤导致该接口 WAN 下 10s+。
	// 无时区字符串按 +08:00 解释（与云控制台展示时区一致，勿改用服务器本地时区）。
	expExpr := bson.M{"$ifNull": bson.A{
		// ISO-8601 字符串（RFC3339 带时区/Z、分钟精度 UTC）与 BSON date 直通
		bson.M{"$convert": bson.M{"input": "$attributes.expired_time", "to": "date", "onError": nil}},
		bson.M{"$dateFromString": bson.M{"dateString": "$attributes.expired_time", "timezone": "+08:00", "onError": nil}},
		bson.M{"$dateFromString": bson.M{"dateString": "$attributes.expired_time", "format": "%Y-%m-%d %H:%M:%S", "timezone": "+08:00", "onError": nil}},
		bson.M{"$dateFromString": bson.M{"dateString": "$attributes.expired_time", "format": "%Y-%m-%d", "timezone": "+08:00", "onError": nil}},
	}}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			// 租户过滤恒定生效：0 不作通配。未选定租户时本查询返回空集
			"tenant_id":               tenantID,
			"attributes.expired_time": bson.M{"$exists": true, "$nin": bson.A{"", nil}},
		}}},
		{{Key: "$addFields", Value: bson.M{"_exp": expExpr}}},
		{{Key: "$match", Value: bson.M{"_exp": bson.M{"$gt": now, "$lte": deadline}}}},
		{{Key: "$facet", Value: bson.M{
			"total": bson.A{bson.M{"$count": "n"}},
			"items": bson.A{
				bson.M{"$sort": bson.M{"_exp": 1}},
				// 瘦身投影：VO 需要 provider/region/status/expired_time，其余肥属性不出网
				bson.M{"$project": bson.M{
					"id": 1, "model_uid": 1, "asset_id": 1, "asset_name": 1,
					"tenant_id": 1, "account_id": 1, "ctime": 1, "utime": 1,
					"attributes.provider":      1,
					"attributes.region":        1,
					"attributes.status":        1,
					"attributes.expired_time": 1,
				}},
				bson.M{"$skip": offset},
				bson.M{"$limit": limit},
			},
		}}},
	}

	cursor, err := d.collection().Aggregate(ctx, pipeline, options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var facet struct {
		Total []struct {
			N int64 `bson:"n"`
		} `bson:"total"`
		Items []Instance `bson:"items"`
	}
	if err := cursor.All(ctx, &facet); err != nil {
		return nil, 0, err
	}

	var total int64
	if len(facet.Total) > 0 {
		total = facet.Total[0].N
	}
	return facet.Items, total, nil
}

// aggregateGroup 通用分组聚合
func (d *dashboardDAO) aggregateGroup(ctx context.Context, tenantID int64, groupField string) ([]GroupCount, error) {
	pipeline := mongo.Pipeline{}

	// match 阶段
	match := bson.M{}
	// 租户过滤恒定生效：0 不作通配。未选定租户时本查询返回空集，
	// 而不是退化为「不加过滤」从而返回全部租户数据。
	match["tenant_id"] = tenantID
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

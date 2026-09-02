package dao

import (
	"context"
	"sort"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/timex"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
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

	// 到期时间落在 attributes.expired_time（执行器同步路径写入的字段名）。
	// 云厂商格式混存多种字符串与 BSON date（见 timex.ParseExpiredTime），
	// 无法在 MongoDB 侧做统一的字符串区间比较，故先粗筛存在性，
	// 再在内存中按解析结果过滤/排序/分页（候选量 ≤ 数千，开销可忽略）。
	filter := bson.M{
		"attributes.expired_time": bson.M{
			"$exists": true,
			"$nin":    bson.A{"", nil},
		},
	}
	// 租户过滤恒定生效：0 不作通配。未选定租户时本查询返回空集，
	// 而不是退化为「不加过滤」从而返回全部租户数据。
	filter["tenant_id"] = tenantID

	cursor, err := d.collection().Find(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var candidates []Instance
	if err := cursor.All(ctx, &candidates); err != nil {
		return nil, 0, err
	}

	expiring := selectExpiring(candidates, now, deadline)

	total := int64(len(expiring))
	if offset >= total {
		return []Instance{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return expiring[offset:end], total, nil
}

// selectExpiring 从候选实例中筛出「即将过期」(now < expire_time <= deadline)
// 并按到期时间升序排序。解析失败（含 0001-01-01 零值）的实例视为无到期时间跳过。
func selectExpiring(candidates []Instance, now, deadline time.Time) []Instance {
	type expiringInstance struct {
		inst  Instance
		expAt time.Time
	}
	filtered := make([]expiringInstance, 0, len(candidates))
	for _, inst := range candidates {
		expAt, ok := timex.ParseExpiredTime(inst.Attributes["expired_time"])
		if !ok {
			continue
		}
		if !expAt.After(now) || expAt.After(deadline) {
			continue
		}
		filtered = append(filtered, expiringInstance{inst: inst, expAt: expAt})
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].expAt.Before(filtered[j].expAt)
	})
	result := make([]Instance, 0, len(filtered))
	for _, it := range filtered {
		result = append(result, it.inst)
	}
	return result
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

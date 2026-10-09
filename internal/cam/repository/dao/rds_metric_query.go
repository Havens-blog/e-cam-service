package dao

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cloudx-sdk/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// RDS 指标读取窗口边界(规格:days 限 1~90;与 NAS/OSS/Disk 读取同口径——
// 日粒度快照,90 天足够覆盖经营趋势回看;常量独立命名避免跨资源语义漂移)
const (
	rdsDefaultReadDays = 30
	rdsMaxReadDays     = 90
)

// normalizeRDSReadDays RDS 指标读取窗口收敛:缺省 30,上限 90
func normalizeRDSReadDays(days int) int {
	if days <= 0 {
		return rdsDefaultReadDays
	}
	if days > rdsMaxReadDays {
		return rdsMaxReadDays
	}
	return days
}

// ListByRDS 取指定账号+rds_id 近 N 天单日指标,按 date 升序(趋势接口 T8)。
// 复用指标读取的窗口口径:date 为 YYYY-MM-DD 字符串,闭区间
// [today-(days-1), today] 即近 N 天(含今日,运营时区)。
func (d *rdsMetricDAO) ListByRDS(ctx context.Context, accountID int64, rdsID string, days int) ([]types.RDSMetric, error) {
	days = normalizeRDSReadDays(days)
	query := bson.M{
		"account_id": accountID,
		"rds_id":     rdsID,
		"date":       bson.M{"$gte": metricDateFloor(days)},
	}
	cursor, err := d.db.Collection(RDSMetricCollection).Find(ctx, query,
		options.Find().SetSort(bson.D{{Key: "date", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("list rds metrics: %w", err)
	}
	defer cursor.Close(ctx)

	var metrics []types.RDSMetric
	if err = cursor.All(ctx, &metrics); err != nil {
		return nil, fmt.Errorf("decode rds metrics: %w", err)
	}
	return metrics, nil
}

// ListByAccounts 取一组账号近 N 天全部指标行(Top 聚合读取 T8,服务层按 rds_id
// 去重与分页)。逐账号 $in 一次取回——行量级 = 实例数 × 天数(≤50×90),
// 内存聚合代价可接受;排序 rds_id+date 升序,方便服务层按实例分组。
func (d *rdsMetricDAO) ListByAccounts(ctx context.Context, accountIDs []int64, days int) ([]types.RDSMetric, error) {
	if len(accountIDs) == 0 {
		return []types.RDSMetric{}, nil
	}
	days = normalizeRDSReadDays(days)
	query := bson.M{
		"account_id": bson.M{"$in": accountIDs},
		"date":       bson.M{"$gte": metricDateFloor(days)},
	}
	cursor, err := d.db.Collection(RDSMetricCollection).Find(ctx, query,
		options.Find().SetSort(bson.D{
			{Key: "rds_id", Value: 1},
			{Key: "date", Value: 1},
		}))
	if err != nil {
		return nil, fmt.Errorf("list rds metrics by accounts: %w", err)
	}
	defer cursor.Close(ctx)

	var metrics []types.RDSMetric
	if err = cursor.All(ctx, &metrics); err != nil {
		return nil, fmt.Errorf("decode rds metrics by accounts: %w", err)
	}
	return metrics, nil
}

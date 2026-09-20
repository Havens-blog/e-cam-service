package dao

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// OSS 指标读取窗口边界(规格:days 限 1~90;与 NAS 读取同口径——日粒度快照,
// 90 天足够覆盖经营趋势回看;上限收敛复用 normalizeNASReadDays 同值语义,此处
// 独立常量命名避免跨资源漂移)
const (
	ossDefaultReadDays = 30
	ossMaxReadDays     = 90
)

// normalizeOSSReadDays OSS 指标读取窗口收敛:缺省 30,上限 90
func normalizeOSSReadDays(days int) int {
	if days <= 0 {
		return ossDefaultReadDays
	}
	if days > ossMaxReadDays {
		return ossMaxReadDays
	}
	return days
}

// ListByBucket 取指定账号+bucket_name 近 N 天单日指标,按 date 升序(趋势接口)。
// 复用指标读取的窗口口径:date 为 YYYY-MM-DD 字符串,闭区间
// [today-(days-1), today] 即近 N 天(含今日,运营时区)。
func (d *ossMetricDAO) ListByBucket(ctx context.Context, accountID int64, bucketName string, days int) ([]types.OSSMetric, error) {
	days = normalizeOSSReadDays(days)
	query := bson.M{
		"account_id":  accountID,
		"bucket_name": bucketName,
		"date":        bson.M{"$gte": metricDateFloor(days)},
	}
	cursor, err := d.db.Collection(OSSMetricCollection).Find(ctx, query,
		options.Find().SetSort(bson.D{{Key: "date", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("list oss metrics: %w", err)
	}
	defer cursor.Close(ctx)

	var metrics []types.OSSMetric
	if err = cursor.All(ctx, &metrics); err != nil {
		return nil, fmt.Errorf("decode oss metrics: %w", err)
	}
	return metrics, nil
}

// ListByAccounts 取一组账号近 N 天全部指标行(Top 聚合读取,服务层按
// bucket_name 去重与分页)。逐账号 $in 一次取回——行量级 = bucket 数 × 天数
// (≤50×90),内存聚合代价可接受;排序 bucket_name+date 升序,方便服务层按
// bucket 分组。
func (d *ossMetricDAO) ListByAccounts(ctx context.Context, accountIDs []int64, days int) ([]types.OSSMetric, error) {
	if len(accountIDs) == 0 {
		return []types.OSSMetric{}, nil
	}
	days = normalizeOSSReadDays(days)
	query := bson.M{
		"account_id": bson.M{"$in": accountIDs},
		"date":       bson.M{"$gte": metricDateFloor(days)},
	}
	cursor, err := d.db.Collection(OSSMetricCollection).Find(ctx, query,
		options.Find().SetSort(bson.D{
			{Key: "bucket_name", Value: 1},
			{Key: "date", Value: 1},
		}))
	if err != nil {
		return nil, fmt.Errorf("list oss metrics by accounts: %w", err)
	}
	defer cursor.Close(ctx)

	var metrics []types.OSSMetric
	if err = cursor.All(ctx, &metrics); err != nil {
		return nil, fmt.Errorf("decode oss metrics by accounts: %w", err)
	}
	return metrics, nil
}

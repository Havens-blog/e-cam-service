package dao

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cloudx-sdk/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// NAS 指标读取窗口边界(规格:days 限 1~90;与 CDN 读取的 366 上限不同,
// NAS 上线初期采集链路粒度为日快照,90 天足够覆盖经营趋势回看)
const (
	nasDefaultReadDays = 30
	nasMaxReadDays     = 90
)

// normalizeNASReadDays NAS 指标读取窗口收敛:缺省 30,上限 90
func normalizeNASReadDays(days int) int {
	if days <= 0 {
		return nasDefaultReadDays
	}
	if days > nasMaxReadDays {
		return nasMaxReadDays
	}
	return days
}

// ListByFs 取指定账号+fs_id 近 N 天单日指标,按 date 升序(趋势接口)。
// 复用 CDN 指标读取的窗口口径:date 为 YYYY-MM-DD 字符串,闭区间
// [today-(days-1), today] 即近 N 天(含今日,运营时区)。
func (d *nasMetricDAO) ListByFs(ctx context.Context, accountID int64, fsID string, days int) ([]types.NASMetric, error) {
	days = normalizeNASReadDays(days)
	query := bson.M{
		"account_id": accountID,
		"fs_id":      fsID,
		"date":       bson.M{"$gte": metricDateFloor(days)},
	}
	cursor, err := d.db.Collection(NASMetricCollection).Find(ctx, query,
		options.Find().SetSort(bson.D{{Key: "date", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("list nas metrics: %w", err)
	}
	defer cursor.Close(ctx)

	var metrics []types.NASMetric
	if err = cursor.All(ctx, &metrics); err != nil {
		return nil, fmt.Errorf("decode nas metrics: %w", err)
	}
	return metrics, nil
}

// ListByAccounts 取一组账号近 N 天全部指标行(Top 聚合读取,服务层按 fs_id
// 去重与分页)。逐账号 $in 一次取回——行量级 = 实例数 × 天数(≤50×90),
// 内存聚合代价可接受;排序 fs_id+date 升序,方便服务层按 fs 分组。
func (d *nasMetricDAO) ListByAccounts(ctx context.Context, accountIDs []int64, days int) ([]types.NASMetric, error) {
	if len(accountIDs) == 0 {
		return []types.NASMetric{}, nil
	}
	days = normalizeNASReadDays(days)
	query := bson.M{
		"account_id": bson.M{"$in": accountIDs},
		"date":       bson.M{"$gte": metricDateFloor(days)},
	}
	cursor, err := d.db.Collection(NASMetricCollection).Find(ctx, query,
		options.Find().SetSort(bson.D{
			{Key: "fs_id", Value: 1},
			{Key: "date", Value: 1},
		}))
	if err != nil {
		return nil, fmt.Errorf("list nas metrics by accounts: %w", err)
	}
	defer cursor.Close(ctx)

	var metrics []types.NASMetric
	if err = cursor.All(ctx, &metrics); err != nil {
		return nil, fmt.Errorf("decode nas metrics by accounts: %w", err)
	}
	return metrics, nil
}

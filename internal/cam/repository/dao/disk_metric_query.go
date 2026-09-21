package dao

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Disk 指标读取窗口边界(规格:days 限 1~90;与 NAS/OSS 读取同口径——日粒度
// 快照,90 天足够覆盖经营趋势回看;常量独立命名避免跨资源语义漂移)
const (
	diskDefaultReadDays = 30
	diskMaxReadDays     = 90
)

// normalizeDiskReadDays Disk 指标读取窗口收敛:缺省 30,上限 90
func normalizeDiskReadDays(days int) int {
	if days <= 0 {
		return diskDefaultReadDays
	}
	if days > diskMaxReadDays {
		return diskMaxReadDays
	}
	return days
}

// ListByDisk 取指定账号+disk_id 近 N 天单日指标,按 date 升序(趋势接口)。
// 复用指标读取的窗口口径:date 为 YYYY-MM-DD 字符串,闭区间
// [today-(days-1), today] 即近 N 天(含今日,运营时区)。
func (d *diskMetricDAO) ListByDisk(ctx context.Context, accountID int64, diskID string, days int) ([]types.DiskMetric, error) {
	days = normalizeDiskReadDays(days)
	query := bson.M{
		"account_id": accountID,
		"disk_id":    diskID,
		"date":       bson.M{"$gte": metricDateFloor(days)},
	}
	cursor, err := d.db.Collection(DiskMetricCollection).Find(ctx, query,
		options.Find().SetSort(bson.D{{Key: "date", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("list disk metrics: %w", err)
	}
	defer cursor.Close(ctx)

	var metrics []types.DiskMetric
	if err = cursor.All(ctx, &metrics); err != nil {
		return nil, fmt.Errorf("decode disk metrics: %w", err)
	}
	return metrics, nil
}

// ListByAccounts 取一组账号近 N 天全部指标行(Top 聚合读取,服务层按 disk_id
// 去重与分页)。逐账号 $in 一次取回——行量级 = 磁盘数 × 天数(≤50×90),
// 内存聚合代价可接受;排序 disk_id+date 升序,方便服务层按磁盘分组。
func (d *diskMetricDAO) ListByAccounts(ctx context.Context, accountIDs []int64, days int) ([]types.DiskMetric, error) {
	if len(accountIDs) == 0 {
		return []types.DiskMetric{}, nil
	}
	days = normalizeDiskReadDays(days)
	query := bson.M{
		"account_id": bson.M{"$in": accountIDs},
		"date":       bson.M{"$gte": metricDateFloor(days)},
	}
	cursor, err := d.db.Collection(DiskMetricCollection).Find(ctx, query,
		options.Find().SetSort(bson.D{
			{Key: "disk_id", Value: 1},
			{Key: "date", Value: 1},
		}))
	if err != nil {
		return nil, fmt.Errorf("list disk metrics by accounts: %w", err)
	}
	defer cursor.Close(ctx)

	var metrics []types.DiskMetric
	if err = cursor.All(ctx, &metrics); err != nil {
		return nil, fmt.Errorf("decode disk metrics by accounts: %w", err)
	}
	return metrics, nil
}

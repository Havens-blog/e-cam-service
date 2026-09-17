package dao

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const CDNMetricCollection = "ecam_cdn_metric"

// 指标读取参数边界:缺省回退 + 上限收敛,防止恶意大范围聚合
const (
	defaultMetricDays  = 30
	maxMetricDays      = 366
	defaultMetricLimit = 10
	maxMetricLimit     = 100
)

// metricCSTZone 运营时区(与采集侧一致,按 Asia/Shanghai 自然日切分)
var metricCSTZone = time.FixedZone("CST", 8*60*60)

// CDNMetricDAO CDN 单日指标数据访问接口
type CDNMetricDAO interface {
	// UpsertMetric 按 (domain, date) 幂等写入单日指标,同日重采覆盖为最新值
	UpsertMetric(ctx context.Context, m types.CDNMetric) error
	// ListByDomain 取指定域名近 N 天单日指标,按 date 降序。
	// accountID > 0 时仅返回该账号的指标(账号级隔离),0 表示不过滤。
	ListByDomain(ctx context.Context, domain string, days int, accountID int64) ([]types.CDNMetric, error)
	// TopByBytes 聚合近 N 天各域名流量(字节求和)后按字节降序取 Top limit。
	// accountID > 0 时仅聚合该账号,0 表示不过滤。
	TopByBytes(ctx context.Context, days, limit int, accountID int64) ([]types.CDNMetricTopRow, error)
}

type cdnMetricDAO struct {
	db *mongox.Mongo
}

// NewCDNMetricDAO 创建 CDN 指标 DAO,并确保 (account_id, domain, date) 唯一索引。
//
// 注意:唯一键必须含 account_id——同一域名可由多家 CDN 账号共同加速(多活 CDN),
// (domain, date) 作为唯一键会让后写的账号覆盖先写账号的行,导致按流量占比的
// 成本归因丢失一方数据。历史部署曾用 (domain, date),此处创建新索引后删除旧索引
// (旧数据中已被覆盖的行无法恢复,重采补回)。
func NewCDNMetricDAO(db *mongox.Mongo) CDNMetricDAO {
	d := &cdnMetricDAO{db: db}
	col := db.Collection(CDNMetricCollection)

	// 1. 创建 (account_id, domain, date) 唯一索引
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "account_id", Value: 1},
			{Key: "domain", Value: 1},
			{Key: "date", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})

	// 2. 清理历史 (domain, date) 唯一索引(若存在),避免其与多账号数据冲突
	//    保留旧索引会导致同一域名跨账号写第二行时违反唯一约束。
	//    DropOne 对不存在的索引名返回 nil error,安全无副作用。
	if _, err := col.Indexes().DropOne(context.Background(), "domain_1_date_1"); err != nil {
		// 索引删除失败(仅在建索引忙时可能)不阻塞启动;留待运维处理,
		// 否则多账号写同域名时会报唯一键冲突,错误信息可见于采集任务日志
		_ = err
	}
	return d
}

func (d *cdnMetricDAO) UpsertMetric(ctx context.Context, m types.CDNMetric) error {
	// 以 (account_id, domain, date) 为键:同一域名可由多家 CDN 账号共同加速,
	// 各账号同日数据独立保留(多活 CDN),互不覆盖。
	filter := bson.M{
		"account_id": m.AccountID,
		"domain":     m.Domain,
		"date":       m.Date,
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

// ListByDomain 取指定域名近 N 天单日指标,按 date 降序
func (d *cdnMetricDAO) ListByDomain(ctx context.Context, domain string, days int, accountID int64) ([]types.CDNMetric, error) {
	query := bson.M{"domain": domain}
	if accountID > 0 {
		query["account_id"] = accountID
	}
	days = normalizeMetricDays(days)
	// date 为 YYYY-MM-DD 字符串,闭区间 [today-(days-1), today] 即近 N 天(含今日)
	query["date"] = bson.M{"$gte": metricDateFloor(days)}

	cursor, err := d.db.Collection(CDNMetricCollection).Find(ctx, query,
		options.Find().SetSort(bson.D{{Key: "date", Value: -1}}))
	if err != nil {
		return nil, fmt.Errorf("list cdn metrics: %w", err)
	}
	defer cursor.Close(ctx)

	var metrics []types.CDNMetric
	if err = cursor.All(ctx, &metrics); err != nil {
		return nil, fmt.Errorf("decode cdn metrics: %w", err)
	}
	return metrics, nil
}

// TopByBytes 聚合近 N 天各域名流量后按字节降序取 Top
func (d *cdnMetricDAO) TopByBytes(ctx context.Context, days, limit int, accountID int64) ([]types.CDNMetricTopRow, error) {
	days = normalizeMetricDays(days)
	limit = normalizeMetricLimit(limit)

	match := bson.M{"date": bson.M{"$gte": metricDateFloor(days)}}
	if accountID > 0 {
		match["account_id"] = accountID
	}
	pipeline := bson.A{
		bson.M{"$match": match},
		bson.M{"$group": bson.M{
			"_id":   "$domain",
			"bytes": bson.M{"$sum": "$bytes"},
			"count": bson.M{"$sum": 1},
		}},
		bson.M{"$sort": bson.M{"bytes": -1}},
		bson.M{"$limit": limit},
	}

	cursor, err := d.db.Collection(CDNMetricCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("top cdn metrics aggregate: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []struct {
		Domain string `bson:"_id"`
		Bytes  int64  `bson:"bytes"`
		Count  int    `bson:"count"`
	}
	if err = cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode cdn metric top: %w", err)
	}

	results := make([]types.CDNMetricTopRow, 0, len(rows))
	for _, r := range rows {
		results = append(results, types.CDNMetricTopRow{
			Domain: r.Domain,
			Bytes:  r.Bytes,
			Days:   days,
			Count:  r.Count,
		})
	}
	return results, nil
}

// normalizeMetricDays 缺省 30 天,上限收敛到 maxMetricDays
func normalizeMetricDays(days int) int {
	if days <= 0 {
		return defaultMetricDays
	}
	if days > maxMetricDays {
		return maxMetricDays
	}
	return days
}

// normalizeMetricLimit 缺省 10 条,上限收敛到 maxMetricLimit
func normalizeMetricLimit(limit int) int {
	if limit <= 0 {
		return defaultMetricLimit
	}
	if limit > maxMetricLimit {
		return maxMetricLimit
	}
	return limit
}

// metricDateFloor 近 N 天窗口的下界日期(含当日,运营时区)
func metricDateFloor(days int) string {
	return time.Now().In(metricCSTZone).AddDate(0, 0, -(days - 1)).Format("2006-01-02")
}

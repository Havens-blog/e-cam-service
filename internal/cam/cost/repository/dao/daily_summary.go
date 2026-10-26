package dao

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-common-go/mongox"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const DailySummaryCollection = "ecam_cost_daily_summary"

// DailySummary 每日成本汇总（预聚合）
type DailySummary struct {
	// 复合主键: billing_date + tenant_id + provider + account_id + service_type + region
	BillingDate string  `bson:"billing_date" json:"billing_date"` // YYYY-MM-DD
	TenantID    int64   `bson:"tenant_id" json:"tenant_id"`
	Provider    string  `bson:"provider" json:"provider"`
	AccountID   int64   `bson:"account_id" json:"account_id"`
	ServiceType string  `bson:"service_type" json:"service_type"`
	Region      string  `bson:"region" json:"region"`
	Amount      float64 `bson:"amount" json:"amount"`
	AmountCNY   float64 `bson:"amount_cny" json:"amount_cny"`
	RecordCount int64   `bson:"record_count" json:"record_count"` // 明细条数
	UpdateTime  int64   `bson:"utime" json:"utime"`
}

// DailySummaryDAO 每日汇总数据访问
type DailySummaryDAO struct {
	db     *mongox.Mongo
	logger *elog.Component
}

// NewDailySummaryDAO 创建每日汇总 DAO
func NewDailySummaryDAO(db *mongox.Mongo, logger *elog.Component) *DailySummaryDAO {
	return &DailySummaryDAO{db: db, logger: logger}
}

// InitIndexes 初始化索引
func (d *DailySummaryDAO) InitIndexes(ctx context.Context) error {
	coll := d.db.Collection(DailySummaryCollection)
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "billing_date", Value: 1},
				{Key: "tenant_id", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "billing_date", Value: 1},
				{Key: "tenant_id", Value: 1},
				{Key: "provider", Value: 1},
			},
		},
	}
	_, err := coll.Indexes().CreateMany(ctx, indexes)
	return err
}

// RebuildFromSource 从明细表重建汇总数据（全量或按日期范围，覆盖全部租户）
func (d *DailySummaryDAO) RebuildFromSource(ctx context.Context, startDate, endDate string) error {
	return d.rebuild(ctx, 0, startDate, endDate)
}

// rebuild 从明细表重建汇总数据。tenantID 为 0 时重建全部租户，否则只重建该租户。
// 使用 MongoDB 聚合管道分批处理，避免一次性加载全部数据。
func (d *DailySummaryDAO) rebuild(ctx context.Context, tenantID int64, startDate, endDate string) error {
	sourceColl := d.db.Collection(UnifiedBillCollection)
	targetColl := d.db.Collection(DailySummaryCollection)

	match := bson.M{}
	if tenantID != 0 {
		match["tenant_id"] = tenantID
	}
	if startDate != "" && endDate != "" {
		match["billing_date"] = bson.M{"$gte": startDate, "$lte": endDate}
	}

	pipeline := bson.A{
		bson.M{"$match": match},
		bson.M{"$group": bson.M{
			"_id": bson.M{
				"billing_date": "$billing_date",
				"tenant_id":    "$tenant_id",
				"provider":     "$provider",
				"account_id":   "$account_id",
				"service_type": "$service_type",
				"region":       "$region",
			},
			"amount":       bson.M{"$sum": "$amount"},
			"amount_cny":   bson.M{"$sum": "$amount_cny"},
			"record_count": bson.M{"$sum": 1},
		}},
	}

	cursor, err := sourceColl.Aggregate(ctx, pipeline,
		options.Aggregate().SetAllowDiskUse(true).SetBatchSize(5000))
	if err != nil {
		return fmt.Errorf("聚合明细表失败: %w", err)
	}
	defer cursor.Close(ctx)

	// 先删除目标范围的旧数据（租户维度重建时只删该租户，避免影响其它租户的汇总）
	if startDate != "" && endDate != "" {
		del := bson.M{
			"billing_date": bson.M{"$gte": startDate, "$lte": endDate},
		}
		if tenantID != 0 {
			del["tenant_id"] = tenantID
		}
		targetColl.DeleteMany(ctx, del)
	}

	now := time.Now().Unix()
	batch := make([]interface{}, 0, 1000)

	for cursor.Next(ctx) {
		var result struct {
			ID struct {
				BillingDate string `bson:"billing_date"`
				TenantID    int64  `bson:"tenant_id"`
				Provider    string `bson:"provider"`
				AccountID   int64  `bson:"account_id"`
				ServiceType string `bson:"service_type"`
				Region      string `bson:"region"`
			} `bson:"_id"`
			Amount      float64 `bson:"amount"`
			AmountCNY   float64 `bson:"amount_cny"`
			RecordCount int64   `bson:"record_count"`
		}
		if err := cursor.Decode(&result); err != nil {
			d.logger.Warn("解码聚合结果失败", elog.FieldErr(err))
			continue
		}

		batch = append(batch, DailySummary{
			BillingDate: result.ID.BillingDate,
			TenantID:    result.ID.TenantID,
			Provider:    result.ID.Provider,
			AccountID:   result.ID.AccountID,
			ServiceType: result.ID.ServiceType,
			Region:      result.ID.Region,
			Amount:      result.Amount,
			AmountCNY:   result.AmountCNY,
			RecordCount: result.RecordCount,
			UpdateTime:  now,
		})

		if len(batch) >= 1000 {
			if _, err := targetColl.InsertMany(ctx, batch); err != nil {
				d.logger.Error("批量写入汇总失败", elog.FieldErr(err))
			}
			batch = batch[:0]
		}
	}

	if len(batch) > 0 {
		if _, err := targetColl.InsertMany(ctx, batch); err != nil {
			d.logger.Error("批量写入汇总失败", elog.FieldErr(err))
		}
	}

	return nil
}

// SumAmount 从汇总表查询总金额
func (d *DailySummaryDAO) SumAmount(ctx context.Context, filter repository.UnifiedBillFilter) (float64, error) {
	query := d.buildQuery(filter)
	pipeline := bson.A{
		bson.M{"$match": query},
		bson.M{"$group": bson.M{
			"_id":        nil,
			"amount_cny": bson.M{"$sum": "$amount_cny"},
		}},
	}

	cursor, err := d.db.Collection(DailySummaryCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	var results []struct {
		AmountCNY float64 `bson:"amount_cny"`
	}
	if err = cursor.All(ctx, &results); err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, nil
	}
	return results[0].AmountCNY, nil
}

// AggregateByField 从汇总表按字段聚合
func (d *DailySummaryDAO) AggregateByField(ctx context.Context, tenantID int64, field string, startDate, endDate string, filter repository.UnifiedBillFilter) ([]repository.AggregateResult, error) {
	match := bson.M{
		"billing_date": bson.M{"$gte": startDate, "$lte": endDate},
	}
	// 租户过滤恒定生效：0 不作通配。未选定租户时本查询返回空集，
	// 而不是退化为「不加过滤」从而返回全部租户数据。
	match["tenant_id"] = tenantID
	if filter.Provider != "" {
		match["provider"] = filter.Provider
	}
	if filter.AccountID > 0 {
		match["account_id"] = filter.AccountID
	}
	if filter.ServiceType != "" {
		match["service_type"] = filter.ServiceType
	}
	if filter.Region != "" {
		match["region"] = filter.Region
	}

	pipeline := bson.A{
		bson.M{"$match": match},
		bson.M{"$group": bson.M{
			"_id":        "$" + field,
			"amount":     bson.M{"$sum": "$amount"},
			"amount_cny": bson.M{"$sum": "$amount_cny"},
		}},
		bson.M{"$sort": bson.M{"amount_cny": -1}},
	}

	cursor, err := d.db.Collection(DailySummaryCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []repository.AggregateResult
	err = cursor.All(ctx, &results)
	return results, err
}

// AggregateDailyAmount 从汇总表按日聚合
func (d *DailySummaryDAO) AggregateDailyAmount(ctx context.Context, tenantID int64, startDate, endDate string, filter repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
	match := bson.M{
		"billing_date": bson.M{"$gte": startDate, "$lte": endDate},
	}
	// 租户过滤恒定生效：0 不作通配。未选定租户时本查询返回空集，
	// 而不是退化为「不加过滤」从而返回全部租户数据。
	match["tenant_id"] = tenantID
	if filter.Provider != "" {
		match["provider"] = filter.Provider
	}
	if filter.AccountID > 0 {
		match["account_id"] = filter.AccountID
	}
	if filter.ServiceType != "" {
		match["service_type"] = filter.ServiceType
	}
	if filter.Region != "" {
		match["region"] = filter.Region
	}

	pipeline := bson.A{
		bson.M{"$match": match},
		bson.M{"$group": bson.M{
			"_id":        "$billing_date",
			"amount":     bson.M{"$sum": "$amount"},
			"amount_cny": bson.M{"$sum": "$amount_cny"},
		}},
		bson.M{"$sort": bson.M{"_id": 1}},
	}

	cursor, err := d.db.Collection(DailySummaryCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []repository.DailyAmount
	err = cursor.All(ctx, &results)
	return results, err
}

// dateRange 返回指定集合 billing_date 的最小/最大值（match 非空时先做过滤）。
// billing_date 为 "YYYY-MM-DD" 定长字符串，字典序即时间序，故 $min/$max 可直接用。
// 集合（或过滤后）为空时返回 ("", "", nil)。
func (d *DailySummaryDAO) dateRange(ctx context.Context, collection string, match bson.M) (string, string, error) {
	pipeline := bson.A{}
	if len(match) > 0 {
		pipeline = append(pipeline, bson.M{"$match": match})
	}
	pipeline = append(pipeline, bson.M{"$group": bson.M{
		"_id": nil,
		"min": bson.M{"$min": "$billing_date"},
		"max": bson.M{"$max": "$billing_date"},
	}})
	cursor, err := d.db.Collection(collection).Aggregate(ctx, pipeline)
	if err != nil {
		return "", "", err
	}
	defer cursor.Close(ctx)

	var results []struct {
		Min string `bson:"min"`
		Max string `bson:"max"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return "", "", err
	}
	if len(results) == 0 {
		return "", "", nil
	}
	return results[0].Min, results[0].Max, nil
}

// shouldRebuildSummary 判断汇总表是否需要从明细表重建（纯函数，便于单测）。
//
// 判据是「账期范围是否与明细表一致」：
//   - 明细表为空（srcMax == ""）→ 无源可依，不动作
//   - 汇总表范围与明细表不一致（含汇总表为空、或只覆盖了陈旧账期）→ 重建
//   - 完全一致 → 跳过，避免每次启动都做一次全量聚合
func shouldRebuildSummary(srcMin, srcMax, sumMin, sumMax string) bool {
	if srcMax == "" {
		return false
	}
	return sumMin != srcMin || sumMax != srcMax
}

// ReconcileWithSource 使汇总表账期范围与明细表对齐（全部租户），返回是否执行了重建。
//
// 这是对原「汇总表为空才重建」闸门（cam/init.go 原先的 HasData 判断）的替换：原闸门
// 一旦写入过历史数据就永不刷新，实测导致汇总表只剩陈旧账期、部分租户整片缺失，
// 成本查询全部退化为明细表全量聚合。改为按账期范围对账后具备自愈能力。
func (d *DailySummaryDAO) ReconcileWithSource(ctx context.Context) (bool, error) {
	return d.reconcile(ctx, 0)
}

// ReconcileTenant 使指定租户的汇总数据与明细表对齐，返回是否执行了重建。
//
// 与 ReconcileWithSource 的区别是只作用于单个租户：供租户维度的运维刷新入口使用。
// 之所以要做成租户维度，是因为全库重建是**跨租户**操作，若把它挂到普通租户可访问的
// 接口上，任何租户都能触发一次全表聚合（既越权又放大攻击面）。
func (d *DailySummaryDAO) ReconcileTenant(ctx context.Context, tenantID int64) (bool, error) {
	if tenantID == 0 {
		// tenant_id=0 在 DAO 里语义是「不过滤租户」，与"只刷新自己"的意图相反，
		// 必须显式拒绝，否则会退化成全库重建（同 cost_handler 组对 tenant_id=0 的防护思路）。
		return false, fmt.Errorf("tenantID 不能为 0")
	}
	return d.reconcile(ctx, tenantID)
}

// reconcile 汇总表对账核心。tenantID 为 0 时对账全部租户，否则只对账该租户。
//
// 顺序上先重建、再清理范围外的残留行：这样即使重建中途失败，旧数据仍在（最差退化为
// 原状），不会把汇总表清空；而范围外的陈旧行必须清掉，否则对账条件永不成立，
// 每次启动都会重复全量重建。
func (d *DailySummaryDAO) reconcile(ctx context.Context, tenantID int64) (bool, error) {
	var scope bson.M
	if tenantID != 0 {
		scope = bson.M{"tenant_id": tenantID}
	}

	srcMin, srcMax, err := d.dateRange(ctx, UnifiedBillCollection, scope)
	if err != nil {
		return false, fmt.Errorf("查询明细表账期范围失败: %w", err)
	}
	sumMin, sumMax, err := d.dateRange(ctx, DailySummaryCollection, scope)
	if err != nil {
		return false, fmt.Errorf("查询汇总表账期范围失败: %w", err)
	}
	if !shouldRebuildSummary(srcMin, srcMax, sumMin, sumMax) {
		return false, nil
	}

	d.logger.Info("汇总表账期与明细表不一致，开始重建",
		elog.Int64("tenant_id", tenantID),
		elog.String("source_min", srcMin), elog.String("source_max", srcMax),
		elog.String("summary_min", sumMin), elog.String("summary_max", sumMax))

	if err := d.rebuild(ctx, tenantID, srcMin, srcMax); err != nil {
		return false, fmt.Errorf("重建汇总表失败: %w", err)
	}

	// 清理范围外的陈旧账期行（best-effort：失败只告警，重建结果已可用）
	outOfRange := bson.M{"$or": bson.A{
		bson.M{"billing_date": bson.M{"$lt": srcMin}},
		bson.M{"billing_date": bson.M{"$gt": srcMax}},
	}}
	if tenantID != 0 {
		outOfRange["tenant_id"] = tenantID
	}
	if _, err := d.db.Collection(DailySummaryCollection).DeleteMany(ctx, outOfRange); err != nil {
		d.logger.Warn("清理汇总表范围外陈旧账期失败", elog.FieldErr(err))
	}

	// 回读重建结果作为可观测收尾证据（范围应等于明细表范围；行数用于确认数据确实落表）
	newMin, newMax, rerr := d.dateRange(ctx, DailySummaryCollection, scope)
	if rerr != nil {
		d.logger.Warn("重建后回读汇总表范围失败", elog.FieldErr(rerr))
	} else {
		rows, cerr := d.db.Collection(DailySummaryCollection).CountDocuments(ctx, scope)
		if cerr != nil {
			rows = -1
		}
		d.logger.Info("汇总表重建完成",
			elog.Int64("tenant_id", tenantID),
			elog.String("summary_min", newMin),
			elog.String("summary_max", newMax),
			elog.Int64("summary_rows", rows))
	}

	return true, nil
}

func (d *DailySummaryDAO) buildQuery(filter repository.UnifiedBillFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != 0 {
		query["tenant_id"] = filter.TenantID
	}
	if filter.Provider != "" {
		query["provider"] = filter.Provider
	}
	if filter.AccountID > 0 {
		query["account_id"] = filter.AccountID
	}
	if filter.ServiceType != "" {
		query["service_type"] = filter.ServiceType
	}
	if filter.Region != "" {
		query["region"] = filter.Region
	}
	if filter.StartDate != "" && filter.EndDate != "" {
		query["billing_date"] = bson.M{"$gte": filter.StartDate, "$lte": filter.EndDate}
	} else if filter.StartDate != "" {
		query["billing_date"] = bson.M{"$gte": filter.StartDate}
	} else if filter.EndDate != "" {
		query["billing_date"] = bson.M{"$lte": filter.EndDate}
	}
	return query
}

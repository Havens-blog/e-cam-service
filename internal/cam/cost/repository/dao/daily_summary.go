package dao

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
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
	TenantID    string  `bson:"tenant_id" json:"tenant_id"`
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

// RebuildFromSource 从明细表重建汇总数据（全量或按日期范围）
// 使用 MongoDB 聚合管道分批处理，避免一次性加载全部数据
func (d *DailySummaryDAO) RebuildFromSource(ctx context.Context, startDate, endDate string) error {
	sourceColl := d.db.Collection(UnifiedBillCollection)
	targetColl := d.db.Collection(DailySummaryCollection)

	match := bson.M{}
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

	// 先删除目标范围的旧数据
	if startDate != "" && endDate != "" {
		targetColl.DeleteMany(ctx, bson.M{
			"billing_date": bson.M{"$gte": startDate, "$lte": endDate},
		})
	}

	now := time.Now().Unix()
	batch := make([]interface{}, 0, 1000)

	for cursor.Next(ctx) {
		var result struct {
			ID struct {
				BillingDate string `bson:"billing_date"`
				TenantID    string `bson:"tenant_id"`
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
func (d *DailySummaryDAO) AggregateByField(ctx context.Context, tenantID string, field string, startDate, endDate string, filter repository.UnifiedBillFilter) ([]repository.AggregateResult, error) {
	match := bson.M{
		"billing_date": bson.M{"$gte": startDate, "$lte": endDate},
	}
	if tenantID != "" {
		match["tenant_id"] = tenantID
	}
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
func (d *DailySummaryDAO) AggregateDailyAmount(ctx context.Context, tenantID string, startDate, endDate string, filter repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
	match := bson.M{
		"billing_date": bson.M{"$gte": startDate, "$lte": endDate},
	}
	if tenantID != "" {
		match["tenant_id"] = tenantID
	}
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

// HasData 检查汇总表是否有数据
func (d *DailySummaryDAO) HasData(ctx context.Context) bool {
	count, err := d.db.Collection(DailySummaryCollection).EstimatedDocumentCount(ctx)
	return err == nil && count > 0
}

func (d *DailySummaryDAO) buildQuery(filter repository.UnifiedBillFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != "" {
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

package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	RawBillCollection     = "cost_raw_bills"
	UnifiedBillCollection = "cost_unified_bills"
)

type billDAO struct {
	db *mongox.Mongo
}

// NewBillDAO 创建账单 DAO
func NewBillDAO(db *mongox.Mongo) repository.BillDAO {
	return &billDAO{db: db}
}

func (d *billDAO) InsertRawBill(ctx context.Context, record domain.RawBillRecord) (int64, error) {
	now := time.Now().UnixMilli()
	record.CreateTime = now
	if record.ID == 0 {
		record.ID = d.db.GetIdGenerator(RawBillCollection)
	}
	_, err := d.db.Collection(RawBillCollection).InsertOne(ctx, record)
	if err != nil {
		return 0, err
	}
	return record.ID, nil
}

func (d *billDAO) InsertRawBills(ctx context.Context, records []domain.RawBillRecord) (int64, error) {
	if len(records) == 0 {
		return 0, nil
	}
	now := time.Now().UnixMilli()
	docs := make([]interface{}, len(records))
	for i := range records {
		records[i].CreateTime = now
		if records[i].ID == 0 {
			records[i].ID = d.db.GetIdGenerator(RawBillCollection)
		}
		docs[i] = records[i]
	}
	result, err := d.db.Collection(RawBillCollection).InsertMany(ctx, docs)
	if err != nil {
		return 0, err
	}
	return int64(len(result.InsertedIDs)), nil
}

func (d *billDAO) GetRawBillByID(ctx context.Context, id int64) (domain.RawBillRecord, error) {
	var record domain.RawBillRecord
	filter := bson.M{"id": id}
	err := d.db.Collection(RawBillCollection).FindOne(ctx, filter).Decode(&record)
	return record, err
}

func (d *billDAO) ListRawBills(ctx context.Context, accountID int64, startDate, endDate string) ([]domain.RawBillRecord, error) {
	filter := bson.M{
		"account_id": accountID,
		"billing_date": bson.M{
			"$gte": startDate,
			"$lte": endDate,
		},
	}
	cursor, err := d.db.Collection(RawBillCollection).Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var records []domain.RawBillRecord
	err = cursor.All(ctx, &records)
	return records, err
}

func (d *billDAO) ListRawBillsByCollectID(ctx context.Context, collectID string) ([]domain.RawBillRecord, error) {
	filter := bson.M{"collect_id": collectID}
	cursor, err := d.db.Collection(RawBillCollection).Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var records []domain.RawBillRecord
	err = cursor.All(ctx, &records)
	return records, err
}

func (d *billDAO) InsertUnifiedBill(ctx context.Context, bill domain.UnifiedBill) (int64, error) {
	now := time.Now().UnixMilli()
	bill.CreateTime = now
	bill.UpdateTime = now
	if bill.ID == 0 {
		bill.ID = d.db.GetIdGenerator(UnifiedBillCollection)
	}
	_, err := d.db.Collection(UnifiedBillCollection).InsertOne(ctx, bill)
	if err != nil {
		return 0, err
	}
	return bill.ID, nil
}

func (d *billDAO) InsertUnifiedBills(ctx context.Context, bills []domain.UnifiedBill) (int64, error) {
	if len(bills) == 0 {
		return 0, nil
	}
	now := time.Now().UnixMilli()
	docs := make([]interface{}, len(bills))
	for i := range bills {
		bills[i].CreateTime = now
		bills[i].UpdateTime = now
		if bills[i].ID == 0 {
			bills[i].ID = d.db.GetIdGenerator(UnifiedBillCollection)
		}
		docs[i] = bills[i]
	}
	result, err := d.db.Collection(UnifiedBillCollection).InsertMany(ctx, docs)
	if err != nil {
		return 0, err
	}
	return int64(len(result.InsertedIDs)), nil
}

func (d *billDAO) GetUnifiedBillByID(ctx context.Context, id int64) (domain.UnifiedBill, error) {
	var bill domain.UnifiedBill
	filter := bson.M{"id": id}
	err := d.db.Collection(UnifiedBillCollection).FindOne(ctx, filter).Decode(&bill)
	return bill, err
}

func (d *billDAO) ListUnifiedBills(ctx context.Context, filter repository.UnifiedBillFilter) ([]domain.UnifiedBill, error) {
	query := d.buildUnifiedBillQuery(filter)
	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "billing_date", Value: -1}, {Key: "ctime", Value: -1}})

	cursor, err := d.db.Collection(UnifiedBillCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var bills []domain.UnifiedBill
	err = cursor.All(ctx, &bills)
	return bills, err
}

func (d *billDAO) CountUnifiedBills(ctx context.Context, filter repository.UnifiedBillFilter) (int64, error) {
	query := d.buildUnifiedBillQuery(filter)
	return d.db.Collection(UnifiedBillCollection).CountDocuments(ctx, query)
}

func (d *billDAO) AggregateByField(ctx context.Context, tenantID string, field string, startDate, endDate string, filter repository.UnifiedBillFilter) ([]repository.AggregateResult, error) {
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

	cursor, err := d.db.Collection(UnifiedBillCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []repository.AggregateResult
	err = cursor.All(ctx, &results)
	return results, err
}

func (d *billDAO) AggregateDailyAmount(ctx context.Context, tenantID string, startDate, endDate string, filter repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
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

	cursor, err := d.db.Collection(UnifiedBillCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []repository.DailyAmount
	err = cursor.All(ctx, &results)
	return results, err
}

func (d *billDAO) SumAmount(ctx context.Context, filter repository.UnifiedBillFilter) (float64, error) {
	query := d.buildUnifiedBillQuery(filter)
	pipeline := bson.A{
		bson.M{"$match": query},
		bson.M{"$group": bson.M{
			"_id":        nil,
			"amount_cny": bson.M{"$sum": "$amount_cny"},
		}},
	}

	cursor, err := d.db.Collection(UnifiedBillCollection).Aggregate(ctx, pipeline)
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

func (d *billDAO) DeleteUnifiedBillsByPeriod(ctx context.Context, tenantID string, period string) error {
	filter := bson.M{
		"tenant_id": tenantID,
		"billing_date": bson.M{
			"$regex": "^" + period, // period is YYYY-MM, billing_date is YYYY-MM-DD
		},
	}
	_, err := d.db.Collection(UnifiedBillCollection).DeleteMany(ctx, filter)
	return err
}

func (d *billDAO) DeleteRawBillsByAccountAndRange(ctx context.Context, accountID int64, startDate, endDate string) (int64, error) {
	filter := bson.M{
		"account_id":   accountID,
		"billing_date": bson.M{"$gte": startDate, "$lte": endDate},
	}
	result, err := d.db.Collection(RawBillCollection).DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

func (d *billDAO) DeleteUnifiedBillsByAccountAndRange(ctx context.Context, accountID int64, startDate, endDate string) (int64, error) {
	filter := bson.M{
		"account_id":   accountID,
		"billing_date": bson.M{"$gte": startDate, "$lte": endDate},
	}
	result, err := d.db.Collection(UnifiedBillCollection).DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

func (d *billDAO) AggregateByTag(ctx context.Context, tenantID string, startDate, endDate string) ([]repository.AggregateResult, error) {
	match := bson.M{
		"billing_date": bson.M{"$gte": startDate, "$lte": endDate},
		"tags":         bson.M{"$ne": nil, "$type": "object"},
	}
	if tenantID != "" {
		match["tenant_id"] = tenantID
	}

	// 将 tags map 展开为 k/v 数组，按 value 聚合金额
	pipeline := bson.A{
		bson.M{"$match": match},
		bson.M{"$project": bson.M{
			"amount_cny": 1,
			"tag_arr":    bson.M{"$objectToArray": "$tags"},
		}},
		bson.M{"$unwind": "$tag_arr"},
		bson.M{"$group": bson.M{
			"_id":        "$tag_arr.v",
			"amount_cny": bson.M{"$sum": "$amount_cny"},
		}},
		bson.M{"$sort": bson.M{"amount_cny": -1}},
	}

	cursor, err := d.db.Collection(UnifiedBillCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []repository.AggregateResult
	err = cursor.All(ctx, &results)
	return results, err
}

func (d *billDAO) buildUnifiedBillQuery(filter repository.UnifiedBillFilter) bson.M {
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
	if filter.ResourceID != "" {
		query["resource_id"] = filter.ResourceID
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

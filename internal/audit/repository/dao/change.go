package dao

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-common-go/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
)

const ChangeHistoryCollection = "ecam_change_history"

// ChangeRecordDAO 变更记录数据访问接口
type ChangeRecordDAO interface {
	BatchCreate(ctx context.Context, records []domain.ChangeRecord) error
	List(ctx context.Context, filter domain.ChangeFilter) ([]domain.ChangeRecord, error)
	Count(ctx context.Context, filter domain.ChangeFilter) (int64, error)
	CountByModelUID(ctx context.Context, filter domain.ChangeFilter) (map[string]int64, error)
	CountByField(ctx context.Context, filter domain.ChangeFilter) (map[string]int64, error)
	CountByProvider(ctx context.Context, filter domain.ChangeFilter) (map[string]int64, error)
	InitIndexes(ctx context.Context) error
}

type changeRecordDAO struct {
	db *mongox.Mongo
}

// NewChangeRecordDAO 创建变更记录 DAO
func NewChangeRecordDAO(db *mongox.Mongo) ChangeRecordDAO {
	return &changeRecordDAO{db: db}
}

// InitIndexes 初始化索引
func (d *changeRecordDAO) InitIndexes(ctx context.Context) error {
	_, err := d.db.Collection(ChangeHistoryCollection).Indexes().CreateMany(ctx, changeHistoryIndexes())
	return err
}

// changeHistoryIndexes 返回 ecam_change_history 的索引定义。
//
// 抽成纯函数，便于用表驱动用例锁住"索引形状"（键顺序），防止后续改动无意删掉某个前缀
// 而悄悄把统计接口重新变成全表扫描。
//
// 【为什么补这三条】（2026-11-04，实测 ecam_change_history 5,399,871 条）
//   - group by model_uid  → 1 分 38 秒
//   - group by field_name → 超过 200 秒仍未返回
//   - 按 field_name 的 CountDocuments → 全表扫描 >30 秒后 Mongo 断开连接
//
// 而 asset_id 有索引时同条件仅 2.9 秒 ⇒ 慢的根因是**缺索引**，不是代码。
//
// 【键顺序依据】GetSummary 的三项统计都是 "$match(租户等值) + $group(按某字段) ，
// 可能附带 ctime 区间"，按 MongoDB 的 ESR 原则排序：
// 等值→分组字段→区间。把 ctime 放在末尾，既能支撑分组，也能在带时间区间时继续用索引收敛范围。
func changeHistoryIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: "asset_id", Value: 1}, {Key: "ctime", Value: -1}}},
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "ctime", Value: -1}}},
		{Keys: bson.D{{Key: "model_uid", Value: 1}, {Key: "ctime", Value: -1}}},
		{Keys: bson.D{{Key: "change_source", Value: 1}}},
		// 补齐的三条：覆盖"租户内按 分组字段 统计"（此前 field_name / provider 完全没有索引）
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "model_uid", Value: 1}, {Key: "ctime", Value: -1}}},
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "field_name", Value: 1}, {Key: "ctime", Value: -1}}},
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "provider", Value: 1}, {Key: "ctime", Value: -1}}},
	}
}

// BatchCreate 批量创建变更记录
func (d *changeRecordDAO) BatchCreate(ctx context.Context, records []domain.ChangeRecord) error {
	if len(records) == 0 {
		return nil
	}
	docs := make([]interface{}, len(records))
	for i, r := range records {
		if r.ID == 0 {
			r.ID = d.db.GetIdGenerator(ChangeHistoryCollection)
		}
		docs[i] = r
	}
	_, err := d.db.Collection(ChangeHistoryCollection).InsertMany(ctx, docs)
	return err
}

// List 查询变更记录列表
func (d *changeRecordDAO) List(ctx context.Context, filter domain.ChangeFilter) ([]domain.ChangeRecord, error) {
	query := d.buildQuery(filter)
	opts := options.Find().SetSort(bson.D{{Key: "ctime", Value: -1}})
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
		opts.SetSkip(filter.Offset)
	}

	cursor, err := d.db.Collection(ChangeHistoryCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var records []domain.ChangeRecord
	if err := cursor.All(ctx, &records); err != nil {
		return nil, err
	}
	return records, nil
}

// Count 统计变更记录数量
func (d *changeRecordDAO) Count(ctx context.Context, filter domain.ChangeFilter) (int64, error) {
	query := d.buildQuery(filter)
	return d.db.Collection(ChangeHistoryCollection).CountDocuments(ctx, query)
}

// CountByModelUID 按模型 UID 统计
func (d *changeRecordDAO) CountByModelUID(ctx context.Context, filter domain.ChangeFilter) (map[string]int64, error) {
	return d.groupCount(ctx, filter, "$model_uid")
}

// CountByField 按字段名统计
func (d *changeRecordDAO) CountByField(ctx context.Context, filter domain.ChangeFilter) (map[string]int64, error) {
	return d.groupCount(ctx, filter, "$field_name")
}

// CountByProvider 按云厂商统计
func (d *changeRecordDAO) CountByProvider(ctx context.Context, filter domain.ChangeFilter) (map[string]int64, error) {
	return d.groupCount(ctx, filter, "$provider")
}

func (d *changeRecordDAO) groupCount(ctx context.Context, filter domain.ChangeFilter, groupField string) (map[string]int64, error) {
	query := d.buildQuery(filter)
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: query}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: groupField},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}

	cursor, err := d.db.Collection(ChangeHistoryCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("聚合变更统计失败: %w", err)
	}
	defer cursor.Close(ctx)

	type groupResult struct {
		ID    string `bson:"_id"`
		Count int64  `bson:"count"`
	}
	var results []groupResult
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	m := make(map[string]int64, len(results))
	for _, r := range results {
		m[r.ID] = r.Count
	}
	return m, nil
}

func (d *changeRecordDAO) buildQuery(filter domain.ChangeFilter) bson.M {
	query := bson.M{}
	if filter.AssetID != "" {
		query["asset_id"] = filter.AssetID
	}
	if filter.TenantID != 0 {
		query["tenant_id"] = filter.TenantID
	}
	if filter.ModelUID != "" {
		query["model_uid"] = filter.ModelUID
	}
	if filter.Provider != "" {
		query["provider"] = filter.Provider
	}
	if filter.FieldName != "" {
		query["field_name"] = filter.FieldName
	}

	timeQuery := bson.M{}
	if filter.StartTime != nil {
		timeQuery["$gte"] = *filter.StartTime
	}
	if filter.EndTime != nil {
		timeQuery["$lte"] = *filter.EndTime
	}
	if len(timeQuery) > 0 {
		query["ctime"] = timeQuery
	}
	return query
}

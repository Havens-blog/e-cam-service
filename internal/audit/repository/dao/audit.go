package dao

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const AuditLogsCollection = "ecam_audit_log"

// AuditLogDAO 审计日志数据访问接口
type AuditLogDAO interface {
	Create(ctx context.Context, log domain.AuditLog) (int64, error)
	List(ctx context.Context, filter domain.AuditLogFilter) ([]domain.AuditLog, error)
	Count(ctx context.Context, filter domain.AuditLogFilter) (int64, error)
	CountByResult(ctx context.Context, filter domain.AuditLogFilter) (map[string]int64, error)
	CountByOperationType(ctx context.Context, filter domain.AuditLogFilter) (map[string]int64, error)
	CountByHTTPMethod(ctx context.Context, filter domain.AuditLogFilter) (map[string]int64, error)
	ListTopEndpoints(ctx context.Context, filter domain.AuditLogFilter, limit int) ([]domain.EndpointStats, error)
	ListTopOperators(ctx context.Context, filter domain.AuditLogFilter, limit int) ([]domain.OperatorStats, error)
	InitIndexes(ctx context.Context) error
}

type auditLogDAO struct {
	db *mongox.Mongo
}

// NewAuditLogDAO 创建审计日志 DAO
func NewAuditLogDAO(db *mongox.Mongo) AuditLogDAO {
	return &auditLogDAO{db: db}
}

// InitIndexes 初始化索引
func (d *auditLogDAO) InitIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "ctime", Value: -1}}},
		{Keys: bson.D{{Key: "api_path", Value: 1}, {Key: "ctime", Value: -1}}},
		{Keys: bson.D{{Key: "request_id", Value: 1}}},
		{Keys: bson.D{{Key: "http_method", Value: 1}, {Key: "ctime", Value: -1}}},
		{Keys: bson.D{{Key: "operator_id", Value: 1}, {Key: "ctime", Value: -1}}},
	}
	_, err := d.db.Collection(AuditLogsCollection).Indexes().CreateMany(ctx, indexes)
	return err
}

// Create 创建审计日志
func (d *auditLogDAO) Create(ctx context.Context, log domain.AuditLog) (int64, error) {
	if log.ID == 0 {
		log.ID = d.db.GetIdGenerator(AuditLogsCollection)
	}
	if log.Ctime == 0 {
		log.Ctime = time.Now().UnixMilli()
	}
	_, err := d.db.Collection(AuditLogsCollection).InsertOne(ctx, log)
	return log.ID, err
}

// List 查询审计日志列表
func (d *auditLogDAO) List(ctx context.Context, filter domain.AuditLogFilter) ([]domain.AuditLog, error) {
	query := d.buildQuery(filter)
	opts := options.Find().SetSort(bson.D{{Key: "ctime", Value: -1}})
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
		opts.SetSkip(filter.Offset)
	}

	cursor, err := d.db.Collection(AuditLogsCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var logs []domain.AuditLog
	if err := cursor.All(ctx, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

// Count 统计审计日志数量
func (d *auditLogDAO) Count(ctx context.Context, filter domain.AuditLogFilter) (int64, error) {
	query := d.buildQuery(filter)
	return d.db.Collection(AuditLogsCollection).CountDocuments(ctx, query)
}

// CountByResult 按结果统计
func (d *auditLogDAO) CountByResult(ctx context.Context, filter domain.AuditLogFilter) (map[string]int64, error) {
	return d.groupCount(ctx, filter, "$result")
}

// CountByOperationType 按操作类型统计
func (d *auditLogDAO) CountByOperationType(ctx context.Context, filter domain.AuditLogFilter) (map[string]int64, error) {
	return d.groupCount(ctx, filter, "$operation_type")
}

// CountByHTTPMethod 按 HTTP 方法统计
func (d *auditLogDAO) CountByHTTPMethod(ctx context.Context, filter domain.AuditLogFilter) (map[string]int64, error) {
	return d.groupCount(ctx, filter, "$http_method")
}

// ListTopEndpoints 获取 Top 端点
func (d *auditLogDAO) ListTopEndpoints(ctx context.Context, filter domain.AuditLogFilter, limit int) ([]domain.EndpointStats, error) {
	query := d.buildQuery(filter)
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: query}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$api_path"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "errors", Value: bson.D{{Key: "$sum", Value: bson.D{
				{Key: "$cond", Value: bson.A{
					bson.D{{Key: "$eq", Value: bson.A{"$result", "failed"}}}, 1, 0,
				}},
			}}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "count", Value: -1}}}},
		{{Key: "$limit", Value: limit}},
		{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 1},
			{Key: "count", Value: 1},
			{Key: "error_rate", Value: bson.D{{Key: "$cond", Value: bson.A{
				bson.D{{Key: "$eq", Value: bson.A{"$count", 0}}}, 0,
				bson.D{{Key: "$divide", Value: bson.A{"$errors", "$count"}}},
			}}}},
		}}},
	}

	cursor, err := d.db.Collection(AuditLogsCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("聚合 Top 端点失败: %w", err)
	}
	defer cursor.Close(ctx)

	var results []domain.EndpointStats
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// ListTopOperators 获取 Top 操作人
func (d *auditLogDAO) ListTopOperators(ctx context.Context, filter domain.AuditLogFilter, limit int) ([]domain.OperatorStats, error) {
	query := d.buildQuery(filter)
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: query}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$operator_id"},
			{Key: "operator_name", Value: bson.D{{Key: "$first", Value: "$operator_name"}}},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "count", Value: -1}}}},
		{{Key: "$limit", Value: limit}},
	}

	cursor, err := d.db.Collection(AuditLogsCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("聚合 Top 操作人失败: %w", err)
	}
	defer cursor.Close(ctx)

	var results []domain.OperatorStats
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// groupCount 通用分组计数
func (d *auditLogDAO) groupCount(ctx context.Context, filter domain.AuditLogFilter, groupField string) (map[string]int64, error) {
	query := d.buildQuery(filter)
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: query}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: groupField},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}

	cursor, err := d.db.Collection(AuditLogsCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
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

// buildQuery 构建查询条件
func (d *auditLogDAO) buildQuery(filter domain.AuditLogFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.OperationType != "" {
		query["operation_type"] = filter.OperationType
	}
	if filter.OperatorID != "" {
		query["operator_id"] = filter.OperatorID
	}
	if filter.HTTPMethod != "" {
		query["http_method"] = filter.HTTPMethod
	}
	if filter.APIPath != "" {
		// 前缀匹配
		query["api_path"] = bson.M{"$regex": "^" + filter.APIPath}
	}
	if filter.RequestID != "" {
		query["request_id"] = filter.RequestID
	}
	if filter.StatusCode > 0 {
		query["status_code"] = filter.StatusCode
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

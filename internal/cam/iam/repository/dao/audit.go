package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const CloudAuditLogsCollection = "cloud_audit_logs"

// AuditOperationType 审计操作类型
type AuditOperationType string

const (
	AuditOpCreateUser       AuditOperationType = "create_user"
	AuditOpUpdateUser       AuditOperationType = "update_user"
	AuditOpDeleteUser       AuditOperationType = "delete_user"
	AuditOpCreateGroup      AuditOperationType = "create_group"
	AuditOpUpdateGroup      AuditOperationType = "update_group"
	AuditOpDeleteGroup      AuditOperationType = "delete_group"
	AuditOpAssignPermission AuditOperationType = "assign_permission"
	AuditOpRevokePermission AuditOperationType = "revoke_permission"
	AuditOpSyncUser         AuditOperationType = "sync_user"
	AuditOpSyncPermission   AuditOperationType = "sync_permission"
)

// AuditResult 审计结果
type AuditResult string

const (
	AuditResultSuccess AuditResult = "success"
	AuditResultFailed  AuditResult = "failed"
)

// AuditLog DAO层审计日志模型
type AuditLog struct {
	ID            int64              `bson:"id"`
	OperationType AuditOperationType `bson:"operation_type"`
	OperatorID    string             `bson:"operator_id"`
	OperatorName  string             `bson:"operator_name"`
	TargetType    string             `bson:"target_type"`
	TargetID      int64              `bson:"target_id"`
	TargetName    string             `bson:"target_name"`
	CloudPlatform CloudProvider      `bson:"cloud_platform"`
	BeforeValue   string             `bson:"before_value"`
	AfterValue    string             `bson:"after_value"`
	Result        AuditResult        `bson:"result"`
	ErrorMessage  string             `bson:"error_message"`
	IPAddress     string             `bson:"ip_address"`
	UserAgent     string             `bson:"user_agent"`
	TenantID      string             `bson:"tenant_id"`
	CreateTime    time.Time          `bson:"create_time"`
	CTime         int64              `bson:"ctime"`
}

// AuditLogFilter DAO层过滤条件
type AuditLogFilter struct {
	OperationType AuditOperationType
	OperatorID    string
	TargetType    string
	CloudPlatform CloudProvider
	TenantID      string
	StartTime     *time.Time
	EndTime       *time.Time
	Offset        int64
	Limit         int64
}

// AuditLogDAO 审计日志数据访问接口
type AuditLogDAO interface {
	Create(ctx context.Context, log AuditLog) (int64, error)
	GetByID(ctx context.Context, id int64) (AuditLog, error)
	List(ctx context.Context, filter AuditLogFilter) ([]AuditLog, error)
	Count(ctx context.Context, filter AuditLogFilter) (int64, error)
	CountByOperationType(ctx context.Context, filter AuditLogFilter) (map[AuditOperationType]int64, error)
	CountByCloudPlatform(ctx context.Context, filter AuditLogFilter) (map[CloudProvider]int64, error)
	CountByResult(ctx context.Context, filter AuditLogFilter) (map[AuditResult]int64, error)
	ListTopOperators(ctx context.Context, filter AuditLogFilter, limit int) ([]OperatorStat, error)
}

// OperatorStat 操作人统计
type OperatorStat struct {
	OperatorID   string `bson:"_id"`
	OperatorName string `bson:"operator_name"`
	OpCount      int64  `bson:"count"`
}

type auditLogDAO struct {
	db *mongox.Mongo
}

// NewAuditLogDAO 创建审计日志DAO
func NewAuditLogDAO(db *mongox.Mongo) AuditLogDAO {
	return &auditLogDAO{
		db: db,
	}
}

// Create 创建审计日志
func (dao *auditLogDAO) Create(ctx context.Context, log AuditLog) (int64, error) {
	now := time.Now()
	nowUnix := now.Unix()

	log.CreateTime = now
	log.CTime = nowUnix

	if log.ID == 0 {
		log.ID = dao.db.GetIdGenerator(CloudAuditLogsCollection)
	}

	_, err := dao.db.Collection(CloudAuditLogsCollection).InsertOne(ctx, log)
	if err != nil {
		return 0, err
	}

	return log.ID, nil
}

// GetByID 根据ID获取审计日志
func (dao *auditLogDAO) GetByID(ctx context.Context, id int64) (AuditLog, error) {
	var log AuditLog
	filter := bson.M{"id": id}

	err := dao.db.Collection(CloudAuditLogsCollection).FindOne(ctx, filter).Decode(&log)
	return log, err
}

// List 获取审计日志列表
func (dao *auditLogDAO) List(ctx context.Context, filter AuditLogFilter) ([]AuditLog, error) {
	var logs []AuditLog

	// 构建查询条件
	query := dao.buildQuery(filter)

	// 设置分页选项
	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.M{"ctime": -1})

	cursor, err := dao.db.Collection(CloudAuditLogsCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	err = cursor.All(ctx, &logs)
	return logs, err
}

// Count 统计审计日志数量
func (dao *auditLogDAO) Count(ctx context.Context, filter AuditLogFilter) (int64, error) {
	query := dao.buildQuery(filter)
	return dao.db.Collection(CloudAuditLogsCollection).CountDocuments(ctx, query)
}

// CountByOperationType 按操作类型统计
func (dao *auditLogDAO) CountByOperationType(ctx context.Context, filter AuditLogFilter) (map[AuditOperationType]int64, error) {
	query := dao.buildQuery(filter)

	pipeline := []bson.M{
		{"$match": query},
		{
			"$group": bson.M{
				"_id":   "$operation_type",
				"count": bson.M{"$sum": 1},
			},
		},
	}

	cursor, err := dao.db.Collection(CloudAuditLogsCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	result := make(map[AuditOperationType]int64)
	for cursor.Next(ctx) {
		var item struct {
			ID    AuditOperationType `bson:"_id"`
			Count int64              `bson:"count"`
		}
		if err := cursor.Decode(&item); err != nil {
			return nil, err
		}
		result[item.ID] = item.Count
	}

	return result, nil
}

// CountByCloudPlatform 按云平台统计
func (dao *auditLogDAO) CountByCloudPlatform(ctx context.Context, filter AuditLogFilter) (map[CloudProvider]int64, error) {
	query := dao.buildQuery(filter)

	pipeline := []bson.M{
		{"$match": query},
		{
			"$group": bson.M{
				"_id":   "$cloud_platform",
				"count": bson.M{"$sum": 1},
			},
		},
	}

	cursor, err := dao.db.Collection(CloudAuditLogsCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	result := make(map[CloudProvider]int64)
	for cursor.Next(ctx) {
		var item struct {
			ID    CloudProvider `bson:"_id"`
			Count int64         `bson:"count"`
		}
		if err := cursor.Decode(&item); err != nil {
			return nil, err
		}
		result[item.ID] = item.Count
	}

	return result, nil
}

// CountByResult 按结果统计
func (dao *auditLogDAO) CountByResult(ctx context.Context, filter AuditLogFilter) (map[AuditResult]int64, error) {
	query := dao.buildQuery(filter)

	pipeline := []bson.M{
		{"$match": query},
		{
			"$group": bson.M{
				"_id":   "$result",
				"count": bson.M{"$sum": 1},
			},
		},
	}

	cursor, err := dao.db.Collection(CloudAuditLogsCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	result := make(map[AuditResult]int64)
	for cursor.Next(ctx) {
		var item struct {
			ID    AuditResult `bson:"_id"`
			Count int64       `bson:"count"`
		}
		if err := cursor.Decode(&item); err != nil {
			return nil, err
		}
		result[item.ID] = item.Count
	}

	return result, nil
}

// ListTopOperators 获取操作最多的用户列表
func (dao *auditLogDAO) ListTopOperators(ctx context.Context, filter AuditLogFilter, limit int) ([]OperatorStat, error) {
	query := dao.buildQuery(filter)

	pipeline := []bson.M{
		{"$match": query},
		{
			"$group": bson.M{
				"_id":           "$operator_id",
				"operator_name": bson.M{"$first": "$operator_name"},
				"count":         bson.M{"$sum": 1},
			},
		},
		{"$sort": bson.M{"count": -1}},
		{"$limit": limit},
	}

	cursor, err := dao.db.Collection(CloudAuditLogsCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var stats []OperatorStat
	err = cursor.All(ctx, &stats)
	return stats, err
}

// buildQuery 构建查询条件
func (dao *auditLogDAO) buildQuery(filter AuditLogFilter) bson.M {
	query := bson.M{}

	if filter.OperationType != "" {
		query["operation_type"] = filter.OperationType
	}
	if filter.OperatorID != "" {
		query["operator_id"] = filter.OperatorID
	}
	if filter.TargetType != "" {
		query["target_type"] = filter.TargetType
	}
	if filter.CloudPlatform != "" {
		query["cloud_platform"] = filter.CloudPlatform
	}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}

	// 时间范围查询
	if filter.StartTime != nil || filter.EndTime != nil {
		timeQuery := bson.M{}
		if filter.StartTime != nil {
			timeQuery["$gte"] = *filter.StartTime
		}
		if filter.EndTime != nil {
			timeQuery["$lte"] = *filter.EndTime
		}
		query["create_time"] = timeQuery
	}

	return query
}

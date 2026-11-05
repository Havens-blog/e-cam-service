package dao

import (
	"context"

	"github.com/Havens-blog/e-common-go/auditlog"
	"github.com/Havens-blog/e-common-go/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AuditEventsCollection 统一审计事件集合（单集合多域：跨域检索的前提是
// 「同一个集合 + 统一 envelope」，集合名与 e-common-go/auditlog SDK 对齐）。
const AuditEventsCollection = auditlog.CollectionName

// AuditEventFilter 统一审计事件查询过滤器。
// 租户恒从登录上下文注入（handler 层取），不作为可选查询参数暴露。
type AuditEventFilter struct {
	TenantID     int64
	ActorID      string
	Service      string
	Domain       string
	Action       string
	Result       string
	ResourceType string
	ResourceID   string
	StartTime    *int64 // Unix 毫秒
	EndTime      *int64 // Unix 毫秒
	Offset       int64
	Limit        int64
	// Order 仅允许 asc（其余回退 desc）；主排序键固定 at（事件时间）。
	Order string
}

// AuditEventDAO 统一审计事件数据访问接口。
// 读侧只暴露 List/Count；写侧由 e-common-go/auditlog 的 MongoWriter 承担
//（append-only，无 update/delete 路径）。
type AuditEventDAO interface {
	List(ctx context.Context, filter *AuditEventFilter) ([]auditlog.AuditEvent, error)
	Count(ctx context.Context, filter *AuditEventFilter) (int64, error)
	InitIndexes(ctx context.Context) error
}

type auditEventDAO struct {
	db *mongox.Mongo
}

// NewAuditEventDAO 创建统一审计事件 DAO
func NewAuditEventDAO(db *mongox.Mongo) AuditEventDAO {
	return &auditEventDAO{db: db}
}

// InitIndexes 建立检索与幂等索引（与 SDK MongoWriter.EnsureIndexes 幂等一致）。
func (d *auditEventDAO) InitIndexes(ctx context.Context) error {
	w := auditlog.NewMongoWriter(d.db.Database())
	return w.EnsureIndexes(ctx)
}

// List 查询统一审计事件（按 at 倒序，asc 显式指定时升序）。
func (d *auditEventDAO) List(ctx context.Context, filter *AuditEventFilter) ([]auditlog.AuditEvent, error) {
	query := buildAuditEventQuery(filter)
	opts := options.Find().SetSort(buildAuditEventSort(filter))
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
		opts.SetSkip(filter.Offset)
	}

	cursor, err := d.db.Collection(AuditEventsCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var events []auditlog.AuditEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, err
	}
	return events, nil
}

// Count 统一审计事件计数。
func (d *auditEventDAO) Count(ctx context.Context, filter *AuditEventFilter) (int64, error) {
	return d.db.Collection(AuditEventsCollection).CountDocuments(ctx, buildAuditEventQuery(filter))
}

// buildAuditEventQuery 构建查询条件。
// 过滤字段名与 auditlog.AuditEvent 的 bson 标签保持一致（写入/读取同源，
// 避免手写字符串键与 SDK 漂移）。
func buildAuditEventQuery(filter *AuditEventFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != 0 {
		query["tenant_id"] = filter.TenantID
	}
	if filter.ActorID != "" {
		query["actor_id"] = filter.ActorID
	}
	if filter.Service != "" {
		query["service"] = filter.Service
	}
	if filter.Domain != "" {
		query["domain"] = filter.Domain
	}
	if filter.Action != "" {
		query["action"] = filter.Action
	}
	if filter.Result != "" {
		query["result"] = filter.Result
	}
	if filter.ResourceType != "" {
		query["resource_type"] = filter.ResourceType
	}
	if filter.ResourceID != "" {
		query["resource_id"] = filter.ResourceID
	}

	timeQuery := bson.M{}
	if filter.StartTime != nil {
		timeQuery["$gte"] = *filter.StartTime
	}
	if filter.EndTime != nil {
		timeQuery["$lte"] = *filter.EndTime
	}
	if len(timeQuery) > 0 {
		query["at"] = timeQuery
	}
	return query
}

// buildAuditEventSort 有序 bson.D 排序（驱动对多键 bson.M 会报
// "multi-key map passed in for ordered parameter sort"）。
func buildAuditEventSort(filter *AuditEventFilter) bson.D {
	dir := -1
	if filter.Order == "asc" {
		dir = 1
	}
	return bson.D{{Key: "at", Value: dir}}
}

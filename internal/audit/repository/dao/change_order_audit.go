package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ChangeOrderAuditCollection 变更单维度审计流水集合（仅追加，7.2）。
const ChangeOrderAuditCollection = "ecam_change_order_audit"

// ChangeOrderAuditDAO 变更单审计流水数据访问接口。
//
// Hard Rule（7.2）：审计记录仅追加、不可修改、不可删除——本接口只有
// Append（写入）与查询方法，禁止添加任何 update/delete 方法。
type ChangeOrderAuditDAO interface {
	// Append 追加一条审计事件（ID/Ctime 未填时补默认）；DedupKey 非空时
	// 经唯一稀疏索引去重，同键已存在返回 inserted=false（幂等契约，
	// 5.9/5.10 端口以 false 抑制重复告警）。
	Append(ctx context.Context, entry domain.ChangeOrderAuditEntry) (id int64, inserted bool, err error)
	// ListByOrder 按单号查询全部事件（at 升序、id 升序稳定返回）。
	ListByOrder(ctx context.Context, orderID string) ([]domain.ChangeOrderAuditEntry, error)
	// ListByOrderAction 按单号+action 查询（at 升序；报告存档投影）。
	ListByOrderAction(ctx context.Context, orderID, action string) ([]domain.ChangeOrderAuditEntry, error)
	// InitIndexes 初始化索引（查询序 + 去重唯一稀疏索引）。
	InitIndexes(ctx context.Context) error
}

type changeOrderAuditDAO struct {
	db *mongox.Mongo
}

// NewChangeOrderAuditDAO 创建变更单审计流水 DAO。
func NewChangeOrderAuditDAO(db *mongox.Mongo) ChangeOrderAuditDAO {
	return &changeOrderAuditDAO{db: db}
}

// InitIndexes 初始化索引：{order_id, at, id} 查询序；{order_id, action,
// dedup_key} 唯一部分索引承载幂等去重（partialFilter 仅覆盖 DedupKey 非空
// 事件——复合稀疏索引会因 order_id/action 恒存在而把缺失键文档以 null 入
// 索引，误伤普通事件重复追加；部分索引精确限定去重域，同 internal/cert
// repository partialIndex 口径）。
func (d *changeOrderAuditDAO) InitIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{Keys: bson.D{
			{Key: "order_id", Value: 1},
			{Key: "at", Value: 1},
			{Key: "id", Value: 1},
		}},
		{
			Keys: bson.D{
				{Key: "order_id", Value: 1},
				{Key: "action", Value: 1},
				{Key: "dedup_key", Value: 1},
			},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{
				"dedup_key": bson.M{"$exists": true},
			}),
		},
	}
	_, err := d.db.Collection(ChangeOrderAuditCollection).Indexes().CreateMany(ctx, indexes)
	return err
}

// Append 追加审计事件；DedupKey 命中唯一索引冲突时返回 inserted=false。
func (d *changeOrderAuditDAO) Append(ctx context.Context, entry domain.ChangeOrderAuditEntry) (int64, bool, error) {
	if entry.ID == 0 {
		entry.ID = d.db.GetIdGenerator(ChangeOrderAuditCollection)
	}
	if entry.At == 0 {
		entry.At = d.nowMillis()
	}
	_, err := d.db.Collection(ChangeOrderAuditCollection).InsertOne(ctx, entry)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return entry.ID, false, nil
		}
		return 0, false, err
	}
	return entry.ID, true, nil
}

// ListByOrder 按单号查询（at 升序、id 升序稳定返回）。
func (d *changeOrderAuditDAO) ListByOrder(ctx context.Context, orderID string) ([]domain.ChangeOrderAuditEntry, error) {
	return d.list(ctx, bson.M{"order_id": orderID})
}

// ListByOrderAction 按单号+action 查询（at 升序）。
func (d *changeOrderAuditDAO) ListByOrderAction(ctx context.Context, orderID, action string) ([]domain.ChangeOrderAuditEntry, error) {
	return d.list(ctx, bson.M{"order_id": orderID, "action": action})
}

func (d *changeOrderAuditDAO) list(ctx context.Context, query bson.M) ([]domain.ChangeOrderAuditEntry, error) {
	opts := options.Find().SetSort(bson.D{
		{Key: "at", Value: 1},
		{Key: "id", Value: 1},
	})
	cursor, err := d.db.Collection(ChangeOrderAuditCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var entries []domain.ChangeOrderAuditEntry
	if err := cursor.All(ctx, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// nowMillis 当前 Unix 毫秒（At 缺省补写）。
func (d *changeOrderAuditDAO) nowMillis() int64 {
	return time.Now().UnixMilli()
}

package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ChangeOrdersCollection 变更单集合名。
const ChangeOrdersCollection = "cert_change_orders"

type changeOrderRepository struct {
	db *mongox.Mongo
}

// NewChangeOrderRepository 创建变更单仓储。
func NewChangeOrderRepository(db *mongox.Mongo) domain.ChangeOrderRepository {
	return &changeOrderRepository{db: db}
}

// Create 写入变更单并返回其 ID（hex）；DEFAULT 填充：createdAt=now。
// 创建路径条件写入：插入时携带 activeMutex（调用方在进入活跃态的单上设置
// ActiveMutex=OldCertFingerprint），uk_active_mutex 部分唯一索引冲突返回
// ErrChangeInFlight——check-then-insert 竞态窗口由索引关闭。
func (r *changeOrderRepository) Create(ctx context.Context, order *domain.ChangeOrder) (string, error) {
	if order.CreatedAt.IsZero() {
		order.CreatedAt = time.Now()
	}
	res, err := r.db.Collection(ChangeOrdersCollection).InsertOne(ctx, order)
	if err != nil {
		return "", mapDupKey(err, domain.ErrChangeInFlight)
	}
	return res.InsertedID.(interface{ Hex() string }).Hex(), nil
}

// GetByID 按文档 ID 查询；未命中返回 mongo.ErrNoDocuments。
func (r *changeOrderRepository) GetByID(ctx context.Context, id string) (domain.ChangeOrder, error) {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return domain.ChangeOrder{}, err
	}
	var order domain.ChangeOrder
	err = r.db.Collection(ChangeOrdersCollection).
		FindOne(ctx, bson.M{"_id": oid}).Decode(&order)
	return order, err
}

// GetByMutexToken 按互斥 token 查活跃单（应用层预检查仅作快速失败，
// 正确性由 uk_active_mutex 部分唯一索引保证）。
func (r *changeOrderRepository) GetByMutexToken(ctx context.Context, token string) (domain.ChangeOrder, error) {
	var order domain.ChangeOrder
	err := r.db.Collection(ChangeOrdersCollection).
		FindOne(ctx, bson.M{"activeMutex": token}).Decode(&order)
	return order, err
}

// ListVerifyingActive 查询验证中的活跃变更单（status=verifying 且
// verifyWindowUntil > after），createdAt 升序稳定返回。
// 任务 4.1 change_linked_diff 判定数据源（探测得 diff 后匹配 verifyExpected）。
func (r *changeOrderRepository) ListVerifyingActive(ctx context.Context, after time.Time) ([]domain.ChangeOrder, error) {
	cursor, err := r.db.Collection(ChangeOrdersCollection).Find(ctx, bson.M{
		"status":            domain.ChangeStatusVerifying,
		"verifyWindowUntil": bson.M{"$gt": after},
	}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var orders []domain.ChangeOrder
	if err := cursor.All(ctx, &orders); err != nil {
		return nil, err
	}
	return orders, nil
}

// TransitionActive 进入活跃态：状态迁移与 activeMutex=token 写入在同一原子 update 完成。
// token 与其他活跃单冲突（uk_active_mutex 部分唯一索引对 update 同样强制）映射
// ErrChangeInFlight——check-then-update 竞态窗口由索引关闭（任务 5.1）。
func (r *changeOrderRepository) TransitionActive(ctx context.Context, id string, to domain.ChangeStatus, mutexToken string) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = r.db.Collection(ChangeOrdersCollection).UpdateOne(ctx,
		bson.M{"_id": oid},
		bson.M{"$set": bson.M{"status": to, "activeMutex": mutexToken}},
	)
	return mapDupKey(err, domain.ErrChangeInFlight)
}

// TransitionTerminal 进入终态：状态迁移与 activeMutex 清除（$unset）在同一原子 update 完成，
// 防止终态单残留 token 阻塞新单（在途互斥活性保障）。
func (r *changeOrderRepository) TransitionTerminal(ctx context.Context, id string, to domain.ChangeStatus) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = r.db.Collection(ChangeOrdersCollection).UpdateOne(ctx,
		bson.M{"_id": oid},
		bson.M{
			"$set":   bson.M{"status": to},
			"$unset": bson.M{"activeMutex": ""},
		},
	)
	return err
}

// TransitionTerminalWithProtect 进入完成类终态（completed/partial_completed）：
// 状态迁移、protectUntil 固化（rollbackProtectDays 派生截止）与 activeMutex 清除
// （$unset）在同一原子 update 完成（任务 5.1：保护期随终态原子生效，token 同步释放）。
func (r *changeOrderRepository) TransitionTerminalWithProtect(ctx context.Context, id string, to domain.ChangeStatus, protectUntil time.Time) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = r.db.Collection(ChangeOrdersCollection).UpdateOne(ctx,
		bson.M{"_id": oid},
		bson.M{
			"$set":   bson.M{"status": to, "protectUntil": protectUntil},
			"$unset": bson.M{"activeMutex": ""},
		},
	)
	return err
}

// ListPausedBefore 批间暂停超时扫描集（任务 5.1 CancelByTimeout）：
// status=executing（批间暂停的常态，排除已取消等终态单重复扫描）且
// batchInfo.paused=true 且 pausedAt 早于 before，createdAt 升序稳定返回。
func (r *changeOrderRepository) ListPausedBefore(ctx context.Context, before time.Time) ([]domain.ChangeOrder, error) {
	cursor, err := r.db.Collection(ChangeOrdersCollection).Find(ctx, bson.M{
		"status":             domain.ChangeStatusExecuting,
		"batchInfo.paused":   true,
		"batchInfo.pausedAt": bson.M{"$lt": before},
	}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var orders []domain.ChangeOrder
	if err := cursor.All(ctx, &orders); err != nil {
		return nil, err
	}
	return orders, nil
}

// ListByNewCertID 按新证书 ID 查询变更单（任务 5.9 orphan-cleanup 归属单判定：
// 新证书映射的孤儿，其归属变更单验证达标/终态后才可清理），createdAt 降序稳定
// 返回（首条=最近归属单，报告承载依据）。
func (r *changeOrderRepository) ListByNewCertID(ctx context.Context, newCertID string) ([]domain.ChangeOrder, error) {
	cursor, err := r.db.Collection(ChangeOrdersCollection).Find(ctx,
		bson.M{"newCertId": newCertID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var orders []domain.ChangeOrder
	if err := cursor.All(ctx, &orders); err != nil {
		return nil, err
	}
	return orders, nil
}

// ListPage 变更单列表分页查询（任务 5.11 GET /changes 状态 Tab 筛选）：
// status 非空时按状态过滤；createdAt 降序 + _id 降序 tie-breaker 稳定返回；
// limit<=0 返回空切片。总数独立 CountDocuments（与页数据同一 filter）。
func (r *changeOrderRepository) ListPage(ctx context.Context, status domain.ChangeStatus, skip, limit int) ([]domain.ChangeOrder, int64, error) {
	filter := bson.M{}
	if status != "" {
		filter["status"] = status
	}
	total, err := r.db.Collection(ChangeOrdersCollection).CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		return []domain.ChangeOrder{}, total, nil
	}
	cursor, err := r.db.Collection(ChangeOrdersCollection).Find(ctx, filter,
		options.Find().
			SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).
			SetSkip(int64(skip)).
			SetLimit(int64(limit)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	var orders []domain.ChangeOrder
	if err := cursor.All(ctx, &orders); err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}

// SetBatchInfo Confirm 固化批次分配（任务 5.7）：CAS status=pending_confirm。
func (r *changeOrderRepository) SetBatchInfo(ctx context.Context, id string, batch *domain.BatchInfo) (bool, error) {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return false, err
	}
	res, err := r.db.Collection(ChangeOrdersCollection).UpdateOne(ctx,
		bson.M{"_id": oid, "status": domain.ChangeStatusPendingConfirm},
		bson.M{"$set": bson.M{"batchInfo": batch}},
	)
	if err != nil {
		return false, err
	}
	return res.ModifiedCount == 1, nil
}

// EnterVerify 进入验证窗口（任务 5.7）：CAS executing→verifying + verifyWindowUntil；
// batchInfo 存在（分批单）时同原子固化批间暂停标记，不存在（未分批单）时以
// $$REMOVE 保持缺失（聚合管道 update，Mongo 4.2+）。
func (r *changeOrderRepository) EnterVerify(ctx context.Context, id string, verifyWindowUntil, pausedAt time.Time) (bool, error) {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return false, err
	}
	// $batchInfo 缺失 → "missing"；$ne 判定后 $mergeObjects 合并暂停标记，
	// 缺失分支返回 $$REMOVE（$set 管道阶段的删除语义——字段保持不存在）。
	pipeline := mongo.Pipeline{bson.D{{Key: "$set", Value: bson.M{
		"status":            domain.ChangeStatusVerifying,
		"verifyWindowUntil": verifyWindowUntil,
		"batchInfo": bson.M{"$cond": bson.A{
			bson.M{"$ne": bson.A{
				bson.M{"$type": bson.M{"$ifNull": bson.A{"$batchInfo", "missing"}}},
				"missing",
			}},
			bson.M{"$mergeObjects": bson.A{
				bson.M{"$ifNull": bson.A{"$batchInfo", bson.M{}}},
				bson.M{"paused": true, "pausedAt": pausedAt},
			}},
			"$$REMOVE",
		}},
	}}}}
	res, err := r.db.Collection(ChangeOrdersCollection).UpdateOne(ctx,
		bson.M{"_id": oid, "status": domain.ChangeStatusExecuting}, pipeline)
	if err != nil {
		return false, err
	}
	return res.ModifiedCount == 1, nil
}

// AdvanceBatch 人工续批放行（任务 5.7 ConfirmBatch）：CAS fromStatus→executing +
// batchInfo.currentBatch=nextBatch + paused=false + $unset pausedAt。
func (r *changeOrderRepository) AdvanceBatch(ctx context.Context, id string, fromStatus domain.ChangeStatus, nextBatch int) (bool, error) {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return false, err
	}
	res, err := r.db.Collection(ChangeOrdersCollection).UpdateOne(ctx,
		bson.M{"_id": oid, "status": fromStatus},
		bson.M{
			"$set": bson.M{
				"status":                 domain.ChangeStatusExecuting,
				"batchInfo.currentBatch": nextBatch,
				"batchInfo.paused":       false,
			},
			"$unset": bson.M{"batchInfo.pausedAt": ""},
		},
	)
	if err != nil {
		return false, err
	}
	return res.ModifiedCount == 1, nil
}

// SetVerifyExpected 固化验证窗口预期终态快照（任务 5.10 AC-1）：CAS
// status=verifying，verifyExpected 与 verifyWindowUntil（=expected.WindowUntil）
// 同一原子 update（扫描与判定两口径一致）；分批单每批进入 verifying 时覆盖刷新。
// 返回值取 MatchedCount（同值重写 ModifiedCount=0 的 Mongo 语义下，CAS 命中
// 即视为固化成功——幂等重写不是并发迁移）。
func (r *changeOrderRepository) SetVerifyExpected(ctx context.Context, id string, expected *domain.VerifyExpected) (bool, error) {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return false, err
	}
	res, err := r.db.Collection(ChangeOrdersCollection).UpdateOne(ctx,
		bson.M{"_id": oid, "status": domain.ChangeStatusVerifying},
		bson.M{"$set": bson.M{
			"verifyExpected":    expected,
			"verifyWindowUntil": expected.WindowUntil,
		}},
	)
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

// PauseAfterVerify 批级窗口收敛回批间暂停（任务 5.10）：CAS verifying→executing +
// batchInfo.paused=true + pausedAt（当前批保持；filter 保证仅分批单命中）。
func (r *changeOrderRepository) PauseAfterVerify(ctx context.Context, id string, pausedAt time.Time) (bool, error) {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return false, err
	}
	res, err := r.db.Collection(ChangeOrdersCollection).UpdateOne(ctx,
		bson.M{"_id": oid, "status": domain.ChangeStatusVerifying, "batchInfo.currentBatch": bson.M{"$exists": true}},
		bson.M{"$set": bson.M{
			"status":             domain.ChangeStatusExecuting,
			"batchInfo.paused":   true,
			"batchInfo.pausedAt": pausedAt,
		}},
	)
	if err != nil {
		return false, err
	}
	return res.ModifiedCount == 1, nil
}

// ListVerifyingExpired 窗口到期扫描集（任务 5.10 window-expiry，Hard Rule：
// 终局判定由 scheduler 主动扫描）：status=verifying 且 verifyWindowUntil 非空
// 且 <= before，createdAt 升序稳定返回。
func (r *changeOrderRepository) ListVerifyingExpired(ctx context.Context, before time.Time) ([]domain.ChangeOrder, error) {
	cursor, err := r.db.Collection(ChangeOrdersCollection).Find(ctx, bson.M{
		"status":            domain.ChangeStatusVerifying,
		"verifyWindowUntil": bson.M{"$ne": nil, "$lte": before},
	}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var orders []domain.ChangeOrder
	if err := cursor.All(ctx, &orders); err != nil {
		return nil, err
	}
	return orders, nil
}

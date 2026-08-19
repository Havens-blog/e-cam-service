package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ChangeItemsCollection 变更项集合名。
const ChangeItemsCollection = "cert_change_items"

type changeItemRepository struct {
	db *mongox.Mongo
}

// NewChangeItemRepository 创建变更项仓储。
func NewChangeItemRepository(db *mongox.Mongo) domain.ChangeItemRepository {
	return &changeItemRepository{db: db}
}

// CreateMulti 批量写入变更项；DEFAULT 填充：status=pending。
func (r *changeItemRepository) CreateMulti(ctx context.Context, items []domain.ChangeItem) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	for i := range items {
		if items[i].Status == "" {
			items[i].Status = domain.ItemStatusPending
		}
	}
	res, err := r.db.Collection(ChangeItemsCollection).InsertMany(ctx, toAnySlice(items))
	if err != nil {
		return 0, err
	}
	return len(res.InsertedIDs), nil
}

// ListByOrder 变更单明细（idx_order）。
func (r *changeItemRepository) ListByOrder(ctx context.Context, orderID string) ([]domain.ChangeItem, error) {
	cursor, err := r.db.Collection(ChangeItemsCollection).
		Find(ctx, bson.M{"orderId": orderID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var items []domain.ChangeItem
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// GetByID 按文档 ID 查询单个变更项（任务 5.7 子任务按项取载）。
func (r *changeItemRepository) GetByID(ctx context.Context, itemID string) (domain.ChangeItem, error) {
	oid, err := objectIDFromHex(itemID)
	if err != nil {
		return domain.ChangeItem{}, err
	}
	var item domain.ChangeItem
	if err := r.db.Collection(ChangeItemsCollection).
		FindOne(ctx, bson.M{"_id": oid}).Decode(&item); err != nil {
		return domain.ChangeItem{}, err
	}
	return item, nil
}

// AssignBatches Confirm 固化批次归属（任务 5.7）：逐项写入 batchNo。
func (r *changeItemRepository) AssignBatches(ctx context.Context, orderID string, assignments []domain.ItemBatchAssignment) error {
	if len(assignments) == 0 {
		return nil
	}
	var models []mongo.WriteModel
	for _, a := range assignments {
		oid, err := objectIDFromHex(a.ItemID)
		if err != nil {
			return err
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": oid, "orderId": orderID}).
			SetUpdate(bson.M{"$set": bson.M{"batchNo": a.BatchNo}}))
	}
	_, err := r.db.Collection(ChangeItemsCollection).BulkWrite(ctx, models)
	return err
}

// ListByOrderAndBatch 当前批执行取项（idx_order_batch，batchNo=batchInfo.currentBatch）。
func (r *changeItemRepository) ListByOrderAndBatch(ctx context.Context, orderID string, batchNo int) ([]domain.ChangeItem, error) {
	cursor, err := r.db.Collection(ChangeItemsCollection).
		Find(ctx, bson.M{"orderId": orderID, "batchNo": batchNo})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var items []domain.ChangeItem
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// UpdateHeartbeat 刷新执行心跳（子任务运行期 30s 间隔更新；
// executing-timeout 任务按 heartbeatAt+itemHeartbeatTimeoutMinutes 判超时）。
func (r *changeItemRepository) UpdateHeartbeat(ctx context.Context, itemID string, at time.Time) error {
	oid, err := objectIDFromHex(itemID)
	if err != nil {
		return err
	}
	_, err = r.db.Collection(ChangeItemsCollection).UpdateOne(ctx,
		bson.M{"_id": oid},
		bson.M{"$set": bson.M{"heartbeatAt": at}},
	)
	return err
}

// MarkPendingSkipped 将订单全部未执行项（status=pending，含未到期批次）标记为
// skipped（任务 5.1 Cancel：整单取消/批间暂停超时取消/执行中止路径共用）；
// running 及已完结项不受影响。返回标记条数。
func (r *changeItemRepository) MarkPendingSkipped(ctx context.Context, orderID string) (int64, error) {
	res, err := r.db.Collection(ChangeItemsCollection).UpdateMany(ctx,
		bson.M{"orderId": orderID, "status": domain.ItemStatusPending},
		bson.M{"$set": bson.M{"status": domain.ItemStatusSkipped}},
	)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// ClaimForExecution 子任务领取执行权（任务 5.7）：CAS pending→running，
// 同原子写 heartbeatAt 与 executedAt。
func (r *changeItemRepository) ClaimForExecution(ctx context.Context, itemID string, at time.Time) (bool, error) {
	oid, err := objectIDFromHex(itemID)
	if err != nil {
		return false, err
	}
	res, err := r.db.Collection(ChangeItemsCollection).UpdateOne(ctx,
		bson.M{"_id": oid, "status": domain.ItemStatusPending},
		bson.M{"$set": bson.M{
			"status":      domain.ItemStatusRunning,
			"heartbeatAt": at,
			"executedAt":  at,
		}},
	)
	if err != nil {
		return false, err
	}
	return res.ModifiedCount == 1, nil
}

// MarkRateLimited 云 API 限流退避标记（任务 5.7）：status→rate_limited 并刷新
// heartbeatAt（退避期间保活）。
func (r *changeItemRepository) MarkRateLimited(ctx context.Context, itemID string, at time.Time) error {
	oid, err := objectIDFromHex(itemID)
	if err != nil {
		return err
	}
	_, err = r.db.Collection(ChangeItemsCollection).UpdateOne(ctx,
		bson.M{"_id": oid, "status": bson.M{"$in": bson.A{domain.ItemStatusRunning, domain.ItemStatusRateLimited}}},
		bson.M{"$set": bson.M{"status": domain.ItemStatusRateLimited, "heartbeatAt": at}},
	)
	return err
}

// FinishItem 项级终态收敛（任务 5.7）：CAS running/rate_limited→终态。
func (r *changeItemRepository) FinishItem(ctx context.Context, itemID string, status domain.ChangeItemStatus, errMsg, newCloudCertID string, at time.Time) (bool, error) {
	oid, err := objectIDFromHex(itemID)
	if err != nil {
		return false, err
	}
	set := bson.M{"status": status, "heartbeatAt": at}
	if errMsg != "" {
		set["error"] = errMsg
	}
	if newCloudCertID != "" {
		set["newCloudCertId"] = newCloudCertID
	}
	res, err := r.db.Collection(ChangeItemsCollection).UpdateOne(ctx,
		bson.M{"_id": oid, "status": bson.M{"$in": bson.A{domain.ItemStatusRunning, domain.ItemStatusRateLimited}}},
		bson.M{"$set": set},
	)
	if err != nil {
		return false, err
	}
	return res.ModifiedCount == 1, nil
}

// FinishRollback 回滚项级终态收敛（任务 5.8）：CAS status=success →
// rolled_back | rollback_failed（同原子写 error；errMsg 空时不覆盖既有字段）。
func (r *changeItemRepository) FinishRollback(ctx context.Context, itemID string, status domain.ChangeItemStatus, errMsg string) (bool, error) {
	if status != domain.ItemStatusRolledBack && status != domain.ItemStatusRollbackFailed {
		return false, fmt.Errorf("change item repo: finish rollback only accepts rolled_back/rollback_failed, got %s", status)
	}
	oid, err := objectIDFromHex(itemID)
	if err != nil {
		return false, err
	}
	set := bson.M{"status": status}
	if errMsg != "" {
		set["error"] = errMsg
	}
	res, err := r.db.Collection(ChangeItemsCollection).UpdateOne(ctx,
		bson.M{"_id": oid, "status": domain.ItemStatusSuccess},
		bson.M{"$set": set},
	)
	if err != nil {
		return false, err
	}
	return res.ModifiedCount == 1, nil
}

// ListPatchCRDDueRecheck patch_crd 项到期复检扫描集（任务 5.9 crd-recheck）：
// status=success 且 recheckedAt 缺失（单轮复检幂等标记）且 executedAt 早于
// before；executedAt 缺失的成功项不构成候选。executedAt 升序稳定返回。
func (r *changeItemRepository) ListPatchCRDDueRecheck(ctx context.Context, before time.Time) ([]domain.ChangeItem, error) {
	cursor, err := r.db.Collection(ChangeItemsCollection).Find(ctx, bson.M{
		"action":      domain.ActionPatchCRD,
		"status":      domain.ItemStatusSuccess,
		"executedAt":  bson.M{"$lte": before},
		"recheckedAt": bson.M{"$exists": false},
	}, options.Find().SetSort(bson.D{{Key: "executedAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var items []domain.ChangeItem
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// MarkRechecked 复检结果回填（任务 5.9）：CAS status=success——passed 保持
// success 仅写 recheckedAt；failed 转 failed 同原子写 error + recheckedAt。
// CAS 未命中返回 false（幂等跳过）。
func (r *changeItemRepository) MarkRechecked(ctx context.Context, itemID string, passed bool, errMsg string, at time.Time) (bool, error) {
	oid, err := objectIDFromHex(itemID)
	if err != nil {
		return false, err
	}
	set := bson.M{"recheckedAt": at}
	if !passed {
		set["status"] = domain.ItemStatusFailed
		if errMsg != "" {
			set["error"] = errMsg
		}
	}
	res, err := r.db.Collection(ChangeItemsCollection).UpdateOne(ctx,
		bson.M{"_id": oid, "status": domain.ItemStatusSuccess},
		bson.M{"$set": set},
	)
	if err != nil {
		return false, err
	}
	return res.ModifiedCount == 1, nil
}

// ListRunningBefore running 且心跳早于 before（或心跳缺失）的变更项
// （executing-timeout 恢复扫描集，任务 5.7）。
func (r *changeItemRepository) ListRunningBefore(ctx context.Context, before time.Time) ([]domain.ChangeItem, error) {
	cursor, err := r.db.Collection(ChangeItemsCollection).Find(ctx, bson.M{
		"status": domain.ItemStatusRunning,
		"$or": bson.A{
			bson.M{"heartbeatAt": bson.M{"$lt": before}},
			bson.M{"heartbeatAt": bson.M{"$exists": false}},
		},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var items []domain.ChangeItem
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}

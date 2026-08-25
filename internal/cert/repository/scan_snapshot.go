package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ScanSnapshotsCollection 扫描快照集合名。
const ScanSnapshotsCollection = "cert_scan_snapshots"

type scanSnapshotRepository struct {
	db *mongox.Mongo
}

// NewScanSnapshotRepository 创建扫描快照仓储。
func NewScanSnapshotRepository(db *mongox.Mongo) domain.ScanSnapshotRepository {
	return &scanSnapshotRepository{db: db}
}

// Create 写入快照并返回其 ID（hex）；DEFAULT 填充：startedAt=now、status=running。
func (r *scanSnapshotRepository) Create(ctx context.Context, snap *domain.ScanSnapshot) (string, error) {
	if snap.StartedAt.IsZero() {
		snap.StartedAt = time.Now()
	}
	if snap.Status == "" {
		snap.Status = domain.ScanStatusRunning
	}
	res, err := r.db.Collection(ScanSnapshotsCollection).InsertOne(ctx, snap)
	if err != nil {
		return "", err
	}
	return res.InsertedID.(interface{ Hex() string }).Hex(), nil
}

// GetByID 按 ID 查询；未命中返回 mongo.ErrNoDocuments。
func (r *scanSnapshotRepository) GetByID(ctx context.Context, id string) (domain.ScanSnapshot, error) {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return domain.ScanSnapshot{}, err
	}
	var snap domain.ScanSnapshot
	err = r.db.Collection(ScanSnapshotsCollection).
		FindOne(ctx, bson.M{"_id": oid}).Decode(&snap)
	return snap, err
}

// LatestDone 最新成功快照（status=done，startedAt 降序取首条，idx_started_at_desc）；
// 无成功快照返回 mongo.ErrNoDocuments（任务 2.3 引用三态派生与 stats 分母数据源）。
func (r *scanSnapshotRepository) LatestDone(ctx context.Context) (domain.ScanSnapshot, error) {
	var snap domain.ScanSnapshot
	err := r.db.Collection(ScanSnapshotsCollection).
		FindOne(ctx, bson.M{"status": domain.ScanStatusDone},
			options.FindOne().SetSort(bson.D{{Key: "startedAt", Value: -1}})).
		Decode(&snap)
	return snap, err
}

// LatestRunning 当前运行中快照（status=running，startedAt 降序取首条）；
// 无运行中快照返回 mongo.ErrNoDocuments（任务 3.5 扫描防重）。
func (r *scanSnapshotRepository) LatestRunning(ctx context.Context) (domain.ScanSnapshot, error) {
	var snap domain.ScanSnapshot
	err := r.db.Collection(ScanSnapshotsCollection).
		FindOne(ctx, bson.M{"status": domain.ScanStatusRunning},
			options.FindOne().SetSort(bson.D{{Key: "startedAt", Value: -1}})).
		Decode(&snap)
	return snap, err
}

// Latest 最新快照（不限状态，startedAt 降序取首条，idx_started_at_desc）；
// 无任何快照返回 mongo.ErrNoDocuments（cert-cloud-discovery-import 任务 3
// snapshot-status 数据源：running/done/failed 均可见）。
func (r *scanSnapshotRepository) Latest(ctx context.Context) (domain.ScanSnapshot, error) {
	var snap domain.ScanSnapshot
	err := r.db.Collection(ScanSnapshotsCollection).
		FindOne(ctx, bson.M{},
			options.FindOne().SetSort(bson.D{{Key: "startedAt", Value: -1}})).
		Decode(&snap)
	return snap, err
}

// ListRunningBefore 运行中且 startedAt 早于 before 的快照（scan-timeout 恢复扫描集）。
func (r *scanSnapshotRepository) ListRunningBefore(ctx context.Context, before time.Time) ([]domain.ScanSnapshot, error) {
	cursor, err := r.db.Collection(ScanSnapshotsCollection).
		Find(ctx, bson.M{"status": domain.ScanStatusRunning, "startedAt": bson.M{"$lt": before}})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var snaps []domain.ScanSnapshot
	if err := cursor.All(ctx, &snaps); err != nil {
		return nil, err
	}
	return snaps, nil
}

// MarkFinished 结束快照：同一原子 update 写 status/failReason/finishedAt
// （scan-timeout 任务转 failed 与扫描完成收敛共用）。
func (r *scanSnapshotRepository) MarkFinished(ctx context.Context, id string, status domain.ScanStatus, failReason string) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	set := bson.M{"status": status, "finishedAt": time.Now()}
	if failReason != "" {
		set["failReason"] = failReason
	}
	_, err = r.db.Collection(ScanSnapshotsCollection).UpdateOne(ctx,
		bson.M{"_id": oid}, bson.M{"$set": set})
	return err
}

// FinishScan 扫描收敛（任务 3.5）：status/failReason/finishedAt 与最终 coverageMeta、
// partialFailures 同一原子 update（covered 固化与终态一致，避免中间态可见）。
func (r *scanSnapshotRepository) FinishScan(ctx context.Context, id string, status domain.ScanStatus, failReason string, meta []domain.CoverageMeta, partials []domain.ScanChannelFailure) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	set := bson.M{
		"status":     status,
		"finishedAt": time.Now(),
	}
	// coverageMeta 空态归一：nil → 空数组（集合校验器 bsonType=array，
	// $set null 会被拒——空范围扫描路径 meta 为 nil，7.1 冒烟实测）。
	if meta != nil {
		set["coverageMeta"] = meta
	} else {
		set["coverageMeta"] = []domain.CoverageMeta{}
	}
	if failReason != "" {
		set["failReason"] = failReason
	}
	if partials != nil {
		set["partialFailures"] = partials
	} else {
		set["partialFailures"] = []domain.ScanChannelFailure{}
	}
	_, err = r.db.Collection(ScanSnapshotsCollection).UpdateOne(ctx,
		bson.M{"_id": oid}, bson.M{"$set": set})
	return err
}

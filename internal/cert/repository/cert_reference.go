package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
)

// CertReferencesCollection 引用扫描发现集合名。
const CertReferencesCollection = "cert_references"

type certReferenceRepository struct {
	db *mongox.Mongo
}

// NewCertReferenceRepository 创建引用扫描发现仓储。
func NewCertReferenceRepository(db *mongox.Mongo) domain.CertReferenceRepository {
	return &certReferenceRepository{db: db}
}

// CreateMulti 批量写入本轮发现引用；DEFAULT 填充：scannedAt=now。
func (r *certReferenceRepository) CreateMulti(ctx context.Context, refs []domain.CertReference) (int, error) {
	if len(refs) == 0 {
		return 0, nil
	}
	now := time.Now()
	for i := range refs {
		if refs[i].ScannedAt.IsZero() {
			refs[i].ScannedAt = now
		}
	}
	res, err := r.db.Collection(CertReferencesCollection).InsertMany(ctx, toAnySlice(refs))
	if err != nil {
		return 0, err
	}
	return len(res.InsertedIDs), nil
}

// ListByFingerprint 按指纹查询全部引用（跨快照累计视图）。
func (r *certReferenceRepository) ListByFingerprint(ctx context.Context, fingerprint string) ([]domain.CertReference, error) {
	cursor, err := r.db.Collection(CertReferencesCollection).
		Find(ctx, bson.M{"certFingerprint": fingerprint})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var refs []domain.CertReference
	if err := cursor.All(ctx, &refs); err != nil {
		return nil, err
	}
	return refs, nil
}

// ListBySnapshotID 按快照查询全部引用（idx_snapshot；任务 2.3 refCount 派生与
// stats 分母聚合数据源）。
func (r *certReferenceRepository) ListBySnapshotID(ctx context.Context, snapshotID string) ([]domain.CertReference, error) {
	cursor, err := r.db.Collection(CertReferencesCollection).
		Find(ctx, bson.M{"snapshotId": snapshotID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var refs []domain.CertReference
	if err := cursor.All(ctx, &refs); err != nil {
		return nil, err
	}
	return refs, nil
}

// DeleteBySnapshotID 按快照清理引用（idx_snapshot）。
func (r *certReferenceRepository) DeleteBySnapshotID(ctx context.Context, snapshotID string) (int64, error) {
	res, err := r.db.Collection(CertReferencesCollection).
		DeleteMany(ctx, bson.M{"snapshotId": snapshotID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// BackfillFingerprint 占位指纹引用回填（任务 4 CAS：filter 含 fromFingerprint，
// 真实指纹引用永不被覆盖；from==to 无操作）。返回回填条数。
func (r *certReferenceRepository) BackfillFingerprint(ctx context.Context, cloud, accountKey, cloudCertID, fromFingerprint, toFingerprint string) (int64, error) {
	if fromFingerprint == toFingerprint {
		return 0, nil
	}
	res, err := r.db.Collection(CertReferencesCollection).UpdateMany(ctx,
		bson.M{
			"cloud":                 cloud,
			"accountKey":            accountKey,
			"referencedCloudCertId": cloudCertID,
			"certFingerprint":       fromFingerprint,
		},
		bson.M{"$set": bson.M{"certFingerprint": toFingerprint}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// toAnySlice 将模型切片转为 InsertMany 所需 []interface{}。
func toAnySlice[T any](in []T) []interface{} {
	out := make([]interface{}, len(in))
	for i := range in {
		out[i] = in[i]
	}
	return out
}

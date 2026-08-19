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

// CloudCertMappingsCollection 平台证书↔云证书映射集合名。
const CloudCertMappingsCollection = "cert_cloud_cert_mappings"

type cloudCertMappingRepository struct {
	db *mongox.Mongo
}

// NewCloudCertMappingRepository 创建云证书映射仓储。
func NewCloudCertMappingRepository(db *mongox.Mongo) domain.CloudCertMappingRepository {
	return &cloudCertMappingRepository{db: db}
}

// Upsert 按 uk_fp_cloud_account（certFingerprint+cloud+accountKey）两段式去重写入；
// DEFAULT 填充：uploadedAt=now、status=active。
func (r *cloudCertMappingRepository) Upsert(ctx context.Context, m *domain.CloudCertMapping) error {
	if m.UploadedAt.IsZero() {
		m.UploadedAt = time.Now()
	}
	if m.Status == "" {
		m.Status = domain.MappingStatusActive
	}
	filter := bson.M{
		"certFingerprint": m.CertFingerprint,
		"cloud":           m.Cloud,
		"accountKey":      m.AccountKey,
	}
	_, err := r.db.Collection(CloudCertMappingsCollection).UpdateOne(ctx,
		filter, bson.M{"$set": m}, optionsUpsert())
	return err
}

// ListByFingerprint 按指纹查询映射（孤儿清理/回滚依据）。
func (r *cloudCertMappingRepository) ListByFingerprint(ctx context.Context, fingerprint string) ([]domain.CloudCertMapping, error) {
	cursor, err := r.db.Collection(CloudCertMappingsCollection).
		Find(ctx, bson.M{"certFingerprint": fingerprint})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var mappings []domain.CloudCertMapping
	if err := cursor.All(ctx, &mappings); err != nil {
		return nil, err
	}
	return mappings, nil
}

// FindByCloudCertID 反查映射（任务 3.5 指纹解析）：cloud/accountKey 空串=通配
// （K8s 引用跨云反查）；多条命中按 uploadedAt 降序取首条（换证后映射刷新）。
func (r *cloudCertMappingRepository) FindByCloudCertID(ctx context.Context, cloud, accountKey, cloudCertID string) (domain.CloudCertMapping, error) {
	filter := bson.M{"cloudCertId": cloudCertID}
	if cloud != "" {
		filter["cloud"] = cloud
	}
	if accountKey != "" {
		filter["accountKey"] = accountKey
	}
	var m domain.CloudCertMapping
	err := r.db.Collection(CloudCertMappingsCollection).
		FindOne(ctx, filter,
			options.FindOne().SetSort(bson.D{{Key: "uploadedAt", Value: -1}})).
		Decode(&m)
	return m, err
}

// UpdateStatus 映射状态迁移（active→orphan，入孤儿清理队列标记）。
func (r *cloudCertMappingRepository) UpdateStatus(ctx context.Context, id string, status domain.MappingStatus) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = r.db.Collection(CloudCertMappingsCollection).UpdateOne(ctx,
		bson.M{"_id": oid},
		bson.M{"$set": bson.M{"status": status}},
	)
	return err
}

// ListByStatus 按状态查询映射（任务 5.9 orphan-cleanup 天级批扫扫描集：
// status=orphan 即清理队列成员），uploadedAt 升序稳定返回（先入队先消费）。
func (r *cloudCertMappingRepository) ListByStatus(ctx context.Context, status domain.MappingStatus) ([]domain.CloudCertMapping, error) {
	cursor, err := r.db.Collection(CloudCertMappingsCollection).Find(ctx,
		bson.M{"status": status},
		options.Find().SetSort(bson.D{{Key: "uploadedAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var mappings []domain.CloudCertMapping
	if err := cursor.All(ctx, &mappings); err != nil {
		return nil, err
	}
	return mappings, nil
}

// DeleteByID 清理成功后删除映射（任务 5.9：orphan→清理成功即删除——
// status enum 仅 active/orphan，"标 cleaned"以删除承载）；非法 hex 返回
// ErrInvalidID，未命中返回 mongo.ErrNoDocuments。
func (r *cloudCertMappingRepository) DeleteByID(ctx context.Context, id string) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	res, err := r.db.Collection(CloudCertMappingsCollection).DeleteOne(ctx, bson.M{"_id": oid})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

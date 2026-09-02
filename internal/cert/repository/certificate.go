package repository

import (
	"context"
	"regexp"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CertificatesCollection 证书台账集合名。
const CertificatesCollection = "cert_certificates"

type certificateRepository struct {
	db *mongox.Mongo
}

// NewCertificateRepository 创建证书台账仓储。
func NewCertificateRepository(db *mongox.Mongo) domain.CertificateRepository {
	return &certificateRepository{db: db}
}

// Create 写入证书；fingerprint 唯一冲突（uk_fingerprint）返回 ErrDuplicateFingerprint。
// DEFAULT 填充：createdAt=now、expiryAlertLevel=none。
func (r *certificateRepository) Create(ctx context.Context, cert *domain.Certificate) error {
	if cert.CreatedAt.IsZero() {
		cert.CreatedAt = time.Now()
	}
	if cert.ExpiryAlertLevel == "" {
		cert.ExpiryAlertLevel = domain.ExpiryAlertNone
	}
	_, err := r.db.Collection(CertificatesCollection).InsertOne(ctx, cert)
	return mapDupKey(err, domain.ErrDuplicateFingerprint)
}

// GetByFingerprint 按指纹查询；未命中返回 mongo.ErrNoDocuments。
func (r *certificateRepository) GetByFingerprint(ctx context.Context, fingerprint string) (domain.Certificate, error) {
	var cert domain.Certificate
	err := r.db.Collection(CertificatesCollection).
		FindOne(ctx, bson.M{"fingerprint": fingerprint}).Decode(&cert)
	return cert, err
}

// GetByID 按文档 ID 查询（详情/补传私钥定位）；非法 ID 返回 ErrInvalidID，
// 未命中返回 mongo.ErrNoDocuments。
func (r *certificateRepository) GetByID(ctx context.Context, id string) (domain.Certificate, error) {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return domain.Certificate{}, err
	}
	var cert domain.Certificate
	err = r.db.Collection(CertificatesCollection).
		FindOne(ctx, bson.M{"_id": oid}).Decode(&cert)
	return cert, err
}

// AttachPrivateKey 补传私钥升级：encryptedPrivateKey 写入与 hostingStatus=complete
// 在同一原子 update 中完成（读侧不会观察到 complete 但无私钥的中间态）。
func (r *certificateRepository) AttachPrivateKey(ctx context.Context, id string, secret *domain.EncryptedSecret) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = r.db.Collection(CertificatesCollection).UpdateOne(ctx,
		bson.M{"_id": oid},
		bson.M{"$set": bson.M{
			"encryptedPrivateKey": secret,
			"hostingStatus":       domain.HostingStatusComplete,
		}},
	)
	return err
}

// UpdateCertPEM 更新证书 PEM 材料（重复指纹幂等导入补链）。
func (r *certificateRepository) UpdateCertPEM(ctx context.Context, id string, certPEM string) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = r.db.Collection(CertificatesCollection).UpdateOne(ctx,
		bson.M{"_id": oid},
		bson.M{"$set": bson.M{"certPem": certPEM}},
	)
	return err
}

// List 全量台账（到期扫描/统计用）。
func (r *certificateRepository) List(ctx context.Context) ([]domain.Certificate, error) {
	cursor, err := r.db.Collection(CertificatesCollection).Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var certs []domain.Certificate
	if err := cursor.All(ctx, &certs); err != nil {
		return nil, err
	}
	return certs, nil
}

// ListPage 服务端分页+筛选（任务 2.3）：notAfter 升序（最快到期优先，_id 升序稳定排序）。
// Search 子串经 QuoteMeta 转义后以不区分大小写 $regex 匹配 commonName/sans/fingerprint
// （数组字段对元素逐一匹配；对齐 internal/cam/tag 的 $regex 用法）。
func (r *certificateRepository) ListPage(ctx context.Context, f domain.CertListFilter, skip, limit int) ([]domain.Certificate, int64, error) {
	filter := bson.M{}
	if f.HostingStatus != "" {
		filter["hostingStatus"] = f.HostingStatus
	}
	notAfter := bson.M{}
	if f.NotAfterFrom != nil {
		notAfter["$gt"] = *f.NotAfterFrom
	}
	if f.NotAfterTo != nil {
		notAfter["$lte"] = *f.NotAfterTo
	}
	if len(notAfter) > 0 {
		filter["notAfter"] = notAfter
	}
	if f.Search != "" {
		re := bson.M{"$regex": regexp.QuoteMeta(f.Search), "$options": "i"}
		filter["$or"] = bson.A{
			bson.M{"commonName": re},
			bson.M{"sans": re},
			bson.M{"fingerprint": re},
		}
	}

	coll := r.db.Collection(CertificatesCollection)
	total, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	if skip < 0 {
		skip = 0
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "notAfter", Value: 1}, {Key: "_id", Value: 1}}).
		SetSkip(int64(skip))
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	var certs []domain.Certificate
	if err := cursor.All(ctx, &certs); err != nil {
		return nil, 0, err
	}
	return certs, total, nil
}

// UpdateExpiryAlertLevel 更新到期分级去重状态（inspection 仅升级触发时调用）。
func (r *certificateRepository) UpdateExpiryAlertLevel(ctx context.Context, fingerprint string, level domain.ExpiryAlertLevel) error {
	_, err := r.db.Collection(CertificatesCollection).UpdateOne(ctx,
		bson.M{"fingerprint": fingerprint},
		bson.M{"$set": bson.M{"expiryAlertLevel": level}},
	)
	return err
}

// SetProtectUntil 设置回滚保护期截止（任务 5.1：变更单进入 completed/
// partial_completed 时按 rollbackProtectDays 固化旧证书保护期，2.3 删除拦截依据）。
// 仅当不存在或新截止更晚时写入（保护期只延长不缩短）；证书不存在时无操作。
func (r *certificateRepository) SetProtectUntil(ctx context.Context, fingerprint string, until time.Time) error {
	_, err := r.db.Collection(CertificatesCollection).UpdateOne(ctx,
		bson.M{
			"fingerprint": fingerprint,
			"$or": bson.A{
				bson.M{"protectUntil": bson.M{"$exists": false}},
				bson.M{"protectUntil": bson.M{"$lt": until}},
			},
		},
		bson.M{"$set": bson.M{"protectUntil": until}},
	)
	return err
}

// DeleteByFingerprint 按指纹删除（保护期/引用拦截由 service 层前置校验）。
func (r *certificateRepository) DeleteByFingerprint(ctx context.Context, fingerprint string) error {
	_, err := r.db.Collection(CertificatesCollection).DeleteOne(ctx, bson.M{"fingerprint": fingerprint})
	return err
}

package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// CrdRegistrationsCollection 自定义 CRD 扫描登记集合名。
const CrdRegistrationsCollection = "cert_crd_registrations"

type crdRegistrationRepository struct {
	db *mongox.Mongo
}

// NewCrdRegistrationRepository 创建 CRD 登记仓储。
func NewCrdRegistrationRepository(db *mongox.Mongo) domain.CrdRegistrationRepository {
	return &crdRegistrationRepository{db: db}
}

// Create 写入登记；clusterId+apiGroup+kind 冲突（uk_cluster_group_kind）返回
// ErrDuplicateCrdRegistration；DEFAULT 填充：createdAt=now、enabled=true
// （登记语义即纳入扫描；停用经 SetEnabled 置 false，该 CRD 回归盲区）。
func (r *crdRegistrationRepository) Create(ctx context.Context, reg *domain.CrdRegistration) error {
	if reg.CreatedAt.IsZero() {
		reg.CreatedAt = time.Now()
	}
	reg.Enabled = true
	if reg.ID.IsZero() {
		// 预生成 _id 回填结构体（driver 仅在 BSON 层补 _id，不回写调用方结构体），
		// 供调用方后续 SetEnabled/DeleteByID 定位文档。
		reg.ID = primitive.NewObjectID()
	}
	_, err := r.db.Collection(CrdRegistrationsCollection).InsertOne(ctx, reg)
	return mapDupKey(err, domain.ErrDuplicateCrdRegistration)
}

// GetByID 按文档 ID 查询；非法 hex 返回 ErrInvalidID，未命中返回 mongo.ErrNoDocuments。
func (r *crdRegistrationRepository) GetByID(ctx context.Context, id string) (domain.CrdRegistration, error) {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return domain.CrdRegistration{}, err
	}
	var reg domain.CrdRegistration
	err = r.db.Collection(CrdRegistrationsCollection).
		FindOne(ctx, bson.M{"_id": oid}).Decode(&reg)
	return reg, err
}

// List 全量登记（运维查看）。
func (r *crdRegistrationRepository) List(ctx context.Context) ([]domain.CrdRegistration, error) {
	cursor, err := r.db.Collection(CrdRegistrationsCollection).Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var regs []domain.CrdRegistration
	if err := cursor.All(ctx, &regs); err != nil {
		return nil, err
	}
	return regs, nil
}

// ListEnabled enabled=true 登记项（K8sAPIChannel 扫描范围联动）。
func (r *crdRegistrationRepository) ListEnabled(ctx context.Context) ([]domain.CrdRegistration, error) {
	cursor, err := r.db.Collection(CrdRegistrationsCollection).
		Find(ctx, bson.M{"enabled": true})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var regs []domain.CrdRegistration
	if err := cursor.All(ctx, &regs); err != nil {
		return nil, err
	}
	return regs, nil
}

// SetEnabled 启停登记（enabled=false 时该 CRD 回归盲区，视图显式声明）。
func (r *crdRegistrationRepository) SetEnabled(ctx context.Context, id string, enabled bool) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = r.db.Collection(CrdRegistrationsCollection).UpdateOne(ctx,
		bson.M{"_id": oid},
		bson.M{"$set": bson.M{"enabled": enabled}},
	)
	return err
}

// DeleteByID 按文档 ID 删除登记。
func (r *crdRegistrationRepository) DeleteByID(ctx context.Context, id string) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = r.db.Collection(CrdRegistrationsCollection).DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

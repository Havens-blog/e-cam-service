package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
)

// BatchSessionsCollection 批量导入会话集合名。
const BatchSessionsCollection = "cert_batch_sessions"

type certBatchSessionRepository struct {
	db *mongox.Mongo
}

// NewCertBatchSessionRepository 创建批量导入会话仓储。
func NewCertBatchSessionRepository(db *mongox.Mongo) domain.CertBatchSessionRepository {
	return &certBatchSessionRepository{db: db}
}

// Create 写入会话并返回 batchId（hex）；DEFAULT 填充：createdAt=now、status=running、
// files 空数组（$jsonSchema 要求 files/progress 必填，nil 切片会序列化为 null 被拒绝）。
func (r *certBatchSessionRepository) Create(ctx context.Context, s *domain.CertBatchSession) (string, error) {
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	if s.Status == "" {
		s.Status = domain.BatchSessionRunning
	}
	if s.Files == nil {
		s.Files = []domain.BatchSessionFile{}
	}
	res, err := r.db.Collection(BatchSessionsCollection).InsertOne(ctx, s)
	if err != nil {
		return "", err
	}
	return res.InsertedID.(interface{ Hex() string }).Hex(), nil
}

// GetByID 按 batchId 查询；未命中返回 mongo.ErrNoDocuments。
func (r *certBatchSessionRepository) GetByID(ctx context.Context, id string) (domain.CertBatchSession, error) {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return domain.CertBatchSession{}, err
	}
	var s domain.CertBatchSession
	err = r.db.Collection(BatchSessionsCollection).
		FindOne(ctx, bson.M{"_id": oid}).Decode(&s)
	return s, err
}

// RecordFileResult 记录单文件结果并原子递增 progress：
// files[fileIndex] 的 result/certId/errorReason 更新与 progress.done/failed 的 $inc
// 在同一原子 update 中完成（进度轮询读到的 files 与 progress 恒一致）。
func (r *certBatchSessionRepository) RecordFileResult(ctx context.Context, id string, fileIndex int, result domain.BatchFileResult, certID, errorReason string) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	set := bson.M{
		fmt.Sprintf("files.%d.result", fileIndex): result,
	}
	if certID != "" {
		set[fmt.Sprintf("files.%d.certId", fileIndex)] = certID
	}
	if errorReason != "" {
		set[fmt.Sprintf("files.%d.errorReason", fileIndex)] = errorReason
	}
	inc := bson.M{}
	switch result {
	case domain.BatchFileSuccess:
		inc["progress.done"] = 1
	case domain.BatchFileFailed:
		inc["progress.failed"] = 1
	}
	update := bson.M{"$set": set}
	if len(inc) > 0 {
		update["$inc"] = inc
	}
	_, err = r.db.Collection(BatchSessionsCollection).UpdateOne(ctx, bson.M{"_id": oid}, update)
	return err
}

// MarkFinished 终态收敛：同一原子 update 写 status/finishedAt。
func (r *certBatchSessionRepository) MarkFinished(ctx context.Context, id string, status domain.BatchSessionStatus) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = r.db.Collection(BatchSessionsCollection).UpdateOne(ctx,
		bson.M{"_id": oid},
		bson.M{"$set": bson.M{"status": status, "finishedAt": time.Now()}},
	)
	return err
}

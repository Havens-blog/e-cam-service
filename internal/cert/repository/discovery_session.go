package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// DiscoveryImportSessionsCollection 云端发现导入会话集合名。
const DiscoveryImportSessionsCollection = "cert_discovery_import_sessions"

type discoveryImportSessionRepository struct {
	db *mongox.Mongo
}

// NewDiscoveryImportSessionRepository 创建云端发现导入会话仓储。
func NewDiscoveryImportSessionRepository(db *mongox.Mongo) domain.DiscoveryImportSessionRepository {
	return &discoveryImportSessionRepository{db: db}
}

// Create 写入会话并返回会话 ID（hex）；DEFAULT 填充：createdAt=now、status=running、
// items 空数组（$jsonSchema 要求 items/progress 必填，nil 切片会序列化为 null 被拒绝）。
func (r *discoveryImportSessionRepository) Create(ctx context.Context, s *domain.DiscoveryImportSession) (string, error) {
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	if s.Status == "" {
		s.Status = domain.DiscoveryImportRunning
	}
	if s.Items == nil {
		s.Items = []domain.DiscoveryImportItem{}
	}
	res, err := r.db.Collection(DiscoveryImportSessionsCollection).InsertOne(ctx, s)
	if err != nil {
		return "", err
	}
	return res.InsertedID.(interface{ Hex() string }).Hex(), nil
}

// GetByID 按会话 ID 查询；非法 hex 返回 ErrInvalidID，未命中返回 mongo.ErrNoDocuments。
func (r *discoveryImportSessionRepository) GetByID(ctx context.Context, id string) (domain.DiscoveryImportSession, error) {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return domain.DiscoveryImportSession{}, err
	}
	var s domain.DiscoveryImportSession
	err = r.db.Collection(DiscoveryImportSessionsCollection).
		FindOne(ctx, bson.M{"_id": oid}).Decode(&s)
	return s, err
}

// RecordItemResult 记录单条目结果并原子递增 progress：
// items[itemIndex] 的 result/mappedCertId/errorReason 更新与 progress.succeeded/failed
// 的 $inc 在同一原子 update 中完成（进度轮询读到的 items 与 progress 恒一致）。
func (r *discoveryImportSessionRepository) RecordItemResult(ctx context.Context, id string, itemIndex int, result domain.DiscoveryItemResult, mappedCertID, errorReason string) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	set := bson.M{
		fmt.Sprintf("items.%d.result", itemIndex): result,
	}
	if mappedCertID != "" {
		set[fmt.Sprintf("items.%d.mappedCertId", itemIndex)] = mappedCertID
	}
	if errorReason != "" {
		set[fmt.Sprintf("items.%d.errorReason", itemIndex)] = errorReason
	}
	inc := bson.M{}
	switch result {
	case domain.DiscoveryItemSuccess:
		inc["progress.succeeded"] = 1
	case domain.DiscoveryItemFailed:
		inc["progress.failed"] = 1
	}
	update := bson.M{"$set": set}
	if len(inc) > 0 {
		update["$inc"] = inc
	}
	_, err = r.db.Collection(DiscoveryImportSessionsCollection).UpdateOne(ctx, bson.M{"_id": oid}, update)
	return err
}

// MarkFinished 终态收敛（按失败计数）：聚合管道更新以库内 progress.failed 判定
// 终态——failed>0 → partial_failed，否则 completed；status 与 finishedAt（$$NOW
// 服务器时间）同一原子 update，无"先读后写"竞态窗口（并发 RecordItemResult 落在
// 收敛前后均不产生中间态脏读）。
func (r *discoveryImportSessionRepository) MarkFinished(ctx context.Context, id string) error {
	oid, err := objectIDFromHex(id)
	if err != nil {
		return err
	}
	update := mongo.Pipeline{
		{{Key: "$set", Value: bson.D{
			{Key: "status", Value: bson.D{
				{Key: "$cond", Value: bson.A{
					bson.D{{Key: "$gt", Value: bson.A{"$progress.failed", 0}}},
					domain.DiscoveryImportPartialFailed,
					domain.DiscoveryImportCompleted,
				}},
			}},
			{Key: "finishedAt", Value: "$$NOW"},
		}}},
	}
	_, err = r.db.Collection(DiscoveryImportSessionsCollection).UpdateOne(ctx, bson.M{"_id": oid}, update)
	return err
}

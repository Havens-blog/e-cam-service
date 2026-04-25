package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const RecommendationCollection = "ecam_cost_recommendation"

type optimizerDAO struct {
	db *mongox.Mongo
}

// NewOptimizerDAO 创建优化建议 DAO
func NewOptimizerDAO(db *mongox.Mongo) repository.OptimizerDAO {
	return &optimizerDAO{db: db}
}

func (d *optimizerDAO) Create(ctx context.Context, rec domain.Recommendation) (int64, error) {
	now := time.Now().UnixMilli()
	rec.CreateTime = now
	rec.UpdateTime = now
	if rec.ID == 0 {
		rec.ID = d.db.GetIdGenerator(RecommendationCollection)
	}
	_, err := d.db.Collection(RecommendationCollection).InsertOne(ctx, rec)
	if err != nil {
		return 0, err
	}
	return rec.ID, nil
}

func (d *optimizerDAO) CreateBatch(ctx context.Context, recs []domain.Recommendation) (int64, error) {
	if len(recs) == 0 {
		return 0, nil
	}
	now := time.Now().UnixMilli()
	docs := make([]interface{}, len(recs))
	for i := range recs {
		recs[i].CreateTime = now
		recs[i].UpdateTime = now
		if recs[i].ID == 0 {
			recs[i].ID = d.db.GetIdGenerator(RecommendationCollection)
		}
		docs[i] = recs[i]
	}
	result, err := d.db.Collection(RecommendationCollection).InsertMany(ctx, docs)
	if err != nil {
		return 0, err
	}
	return int64(len(result.InsertedIDs)), nil
}

func (d *optimizerDAO) GetByID(ctx context.Context, id int64) (domain.Recommendation, error) {
	var rec domain.Recommendation
	filter := bson.M{"id": id}
	err := d.db.Collection(RecommendationCollection).FindOne(ctx, filter).Decode(&rec)
	return rec, err
}

func (d *optimizerDAO) Update(ctx context.Context, rec domain.Recommendation) error {
	rec.UpdateTime = time.Now().UnixMilli()
	filter := bson.M{"id": rec.ID}
	update := bson.M{"$set": bson.M{
		"status":         rec.Status,
		"dismissed_at":   rec.DismissedAt,
		"dismiss_expiry": rec.DismissExpiry,
		"utime":          rec.UpdateTime,
	}}
	result, err := d.db.Collection(RecommendationCollection).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (d *optimizerDAO) List(ctx context.Context, filter repository.RecommendationFilter) ([]domain.Recommendation, error) {
	query := d.buildQuery(filter)
	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "estimated_saving", Value: -1}, {Key: "ctime", Value: -1}})

	cursor, err := d.db.Collection(RecommendationCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var recs []domain.Recommendation
	err = cursor.All(ctx, &recs)
	return recs, err
}

func (d *optimizerDAO) Count(ctx context.Context, filter repository.RecommendationFilter) (int64, error) {
	query := d.buildQuery(filter)
	return d.db.Collection(RecommendationCollection).CountDocuments(ctx, query)
}

func (d *optimizerDAO) FindByResourceAndType(ctx context.Context, tenantID, resourceID, recType string) (domain.Recommendation, error) {
	var rec domain.Recommendation
	filter := bson.M{
		"tenant_id":   tenantID,
		"resource_id": resourceID,
		"type":        recType,
	}
	opts := options.FindOne().SetSort(bson.D{{Key: "ctime", Value: -1}})
	err := d.db.Collection(RecommendationCollection).FindOne(ctx, filter, opts).Decode(&rec)
	return rec, err
}

func (d *optimizerDAO) buildQuery(filter repository.RecommendationFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.Type != "" {
		query["type"] = filter.Type
	}
	if filter.Provider != "" {
		query["provider"] = filter.Provider
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.ExcludeDismiss {
		now := time.Now()
		query["$or"] = bson.A{
			bson.M{"status": bson.M{"$ne": "dismissed"}},
			bson.M{"dismiss_expiry": bson.M{"$lt": now}},
		}
	}
	return query
}

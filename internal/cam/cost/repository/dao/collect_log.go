package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const CollectLogCollection = "cost_collect_logs"

type collectLogDAO struct {
	db *mongox.Mongo
}

// NewCollectLogDAO 创建采集日志 DAO
func NewCollectLogDAO(db *mongox.Mongo) repository.CollectLogDAO {
	return &collectLogDAO{db: db}
}

func (d *collectLogDAO) Create(ctx context.Context, log domain.CollectLog) (int64, error) {
	now := time.Now().UnixMilli()
	log.CreateTime = now
	if log.ID == 0 {
		log.ID = d.db.GetIdGenerator(CollectLogCollection)
	}
	_, err := d.db.Collection(CollectLogCollection).InsertOne(ctx, log)
	if err != nil {
		return 0, err
	}
	return log.ID, nil
}

func (d *collectLogDAO) Update(ctx context.Context, log domain.CollectLog) error {
	filter := bson.M{"id": log.ID}
	update := bson.M{"$set": bson.M{
		"status":       log.Status,
		"end_time":     log.EndTime,
		"record_count": log.RecordCount,
		"duration_ms":  log.Duration,
		"error_msg":    log.ErrorMsg,
	}}
	_, err := d.db.Collection(CollectLogCollection).UpdateOne(ctx, filter, update)
	return err
}

func (d *collectLogDAO) GetByID(ctx context.Context, id int64) (domain.CollectLog, error) {
	var log domain.CollectLog
	filter := bson.M{"id": id}
	err := d.db.Collection(CollectLogCollection).FindOne(ctx, filter).Decode(&log)
	return log, err
}

func (d *collectLogDAO) GetLastSuccess(ctx context.Context, accountID int64) (domain.CollectLog, error) {
	var log domain.CollectLog
	filter := bson.M{
		"account_id": accountID,
		"status":     "success",
	}
	opts := options.FindOne().SetSort(bson.D{{Key: "ctime", Value: -1}})
	err := d.db.Collection(CollectLogCollection).FindOne(ctx, filter, opts).Decode(&log)
	return log, err
}

func (d *collectLogDAO) GetLastFailed(ctx context.Context, accountID int64) (domain.CollectLog, error) {
	var log domain.CollectLog
	filter := bson.M{
		"account_id": accountID,
		"status":     "failed",
	}
	opts := options.FindOne().SetSort(bson.D{{Key: "ctime", Value: -1}})
	err := d.db.Collection(CollectLogCollection).FindOne(ctx, filter, opts).Decode(&log)
	return log, err
}

func (d *collectLogDAO) List(ctx context.Context, filter repository.CollectLogFilter) ([]domain.CollectLog, error) {
	query := d.buildQuery(filter)
	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "ctime", Value: -1}})

	cursor, err := d.db.Collection(CollectLogCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var logs []domain.CollectLog
	err = cursor.All(ctx, &logs)
	return logs, err
}

func (d *collectLogDAO) Count(ctx context.Context, filter repository.CollectLogFilter) (int64, error) {
	query := d.buildQuery(filter)
	return d.db.Collection(CollectLogCollection).CountDocuments(ctx, query)
}

func (d *collectLogDAO) buildQuery(filter repository.CollectLogFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.AccountID > 0 {
		query["account_id"] = filter.AccountID
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	return query
}

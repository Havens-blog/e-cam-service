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

const AnomalyCollection = "cost_anomalies"

type anomalyDAO struct {
	db *mongox.Mongo
}

// NewAnomalyDAO 创建异常事件 DAO
func NewAnomalyDAO(db *mongox.Mongo) repository.AnomalyDAO {
	return &anomalyDAO{db: db}
}

func (d *anomalyDAO) Create(ctx context.Context, anomaly domain.CostAnomaly) (int64, error) {
	now := time.Now().UnixMilli()
	anomaly.CreateTime = now
	if anomaly.ID == 0 {
		anomaly.ID = d.db.GetIdGenerator(AnomalyCollection)
	}
	_, err := d.db.Collection(AnomalyCollection).InsertOne(ctx, anomaly)
	if err != nil {
		return 0, err
	}
	return anomaly.ID, nil
}

func (d *anomalyDAO) CreateBatch(ctx context.Context, anomalies []domain.CostAnomaly) (int64, error) {
	if len(anomalies) == 0 {
		return 0, nil
	}
	now := time.Now().UnixMilli()
	docs := make([]interface{}, len(anomalies))
	for i := range anomalies {
		anomalies[i].CreateTime = now
		if anomalies[i].ID == 0 {
			anomalies[i].ID = d.db.GetIdGenerator(AnomalyCollection)
		}
		docs[i] = anomalies[i]
	}
	result, err := d.db.Collection(AnomalyCollection).InsertMany(ctx, docs)
	if err != nil {
		return 0, err
	}
	return int64(len(result.InsertedIDs)), nil
}

func (d *anomalyDAO) GetByID(ctx context.Context, id int64) (domain.CostAnomaly, error) {
	var anomaly domain.CostAnomaly
	filter := bson.M{"id": id}
	err := d.db.Collection(AnomalyCollection).FindOne(ctx, filter).Decode(&anomaly)
	return anomaly, err
}

func (d *anomalyDAO) List(ctx context.Context, filter repository.AnomalyFilter) ([]domain.CostAnomaly, error) {
	query := d.buildQuery(filter)
	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}

	// 排序：按严重程度或日期
	sort := bson.D{}
	if filter.SortBy == "severity" {
		// 自定义严重程度排序：critical > warning > info
		// 使用 deviation_pct 降序作为近似排序
		sort = append(sort, bson.E{Key: "deviation_pct", Value: -1})
	}
	sort = append(sort, bson.E{Key: "anomaly_date", Value: -1}, bson.E{Key: "ctime", Value: -1})
	opts.SetSort(sort)

	cursor, err := d.db.Collection(AnomalyCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var anomalies []domain.CostAnomaly
	err = cursor.All(ctx, &anomalies)
	return anomalies, err
}

func (d *anomalyDAO) Count(ctx context.Context, filter repository.AnomalyFilter) (int64, error) {
	query := d.buildQuery(filter)
	return d.db.Collection(AnomalyCollection).CountDocuments(ctx, query)
}

func (d *anomalyDAO) buildQuery(filter repository.AnomalyFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.Dimension != "" {
		query["dimension"] = filter.Dimension
	}
	if filter.Severity != "" {
		query["severity"] = filter.Severity
	}
	if filter.StartDate != "" && filter.EndDate != "" {
		query["anomaly_date"] = bson.M{"$gte": filter.StartDate, "$lte": filter.EndDate}
	} else if filter.StartDate != "" {
		query["anomaly_date"] = bson.M{"$gte": filter.StartDate}
	} else if filter.EndDate != "" {
		query["anomaly_date"] = bson.M{"$lte": filter.EndDate}
	}
	return query
}

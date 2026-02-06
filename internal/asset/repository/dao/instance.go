// Package dao 资产数据访问层
package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const InstanceCollection = "c_instance"

// Instance DAO层资产实例模型
type Instance struct {
	ID         int64                  `bson:"id"`
	ModelUID   string                 `bson:"model_uid"`
	AssetID    string                 `bson:"asset_id"`
	AssetName  string                 `bson:"asset_name"`
	TenantID   string                 `bson:"tenant_id"`
	AccountID  int64                  `bson:"account_id"`
	Attributes map[string]interface{} `bson:"attributes"`
	Ctime      int64                  `bson:"ctime"`
	Utime      int64                  `bson:"utime"`
}

// InstanceFilter DAO层过滤条件
type InstanceFilter struct {
	ModelUID   string
	TenantID   string
	AccountID  int64
	AssetID    string
	AssetName  string
	Provider   string
	TagFilter  *TagFilter
	Attributes map[string]interface{}
	Offset     int64
	Limit      int64
}

// TagFilter 标签过滤条件
type TagFilter struct {
	HasTags bool
	NoTags  bool
	Key     string
	Value   string
}

// SearchFilter 统一搜索过滤条件
type SearchFilter struct {
	TenantID   string
	Keyword    string
	AssetTypes []string
	Provider   string
	AccountID  int64
	Region     string
	Offset     int64
	Limit      int64
}

// InstanceDAO 资产实例数据访问接口
type InstanceDAO interface {
	Create(ctx context.Context, instance Instance) (int64, error)
	CreateBatch(ctx context.Context, instances []Instance) (int64, error)
	Update(ctx context.Context, instance Instance) error
	GetByID(ctx context.Context, id int64) (Instance, error)
	GetByAssetID(ctx context.Context, tenantID, modelUID, assetID string) (Instance, error)
	List(ctx context.Context, filter InstanceFilter) ([]Instance, error)
	Count(ctx context.Context, filter InstanceFilter) (int64, error)
	Delete(ctx context.Context, id int64) error
	DeleteByAccountID(ctx context.Context, accountID int64) error
	DeleteByAssetIDs(ctx context.Context, tenantID, modelUID string, assetIDs []string) (int64, error)
	ListAssetIDsByRegion(ctx context.Context, tenantID, modelUID string, accountID int64, region string) ([]string, error)
	ListAssetIDsByModelUID(ctx context.Context, tenantID, modelUID string, accountID int64) ([]string, error)
	Upsert(ctx context.Context, instance Instance) error
	Search(ctx context.Context, filter SearchFilter) ([]Instance, int64, error)
}

type instanceDAO struct {
	db *mongox.Mongo
}

func NewInstanceDAO(db *mongox.Mongo) InstanceDAO {
	return &instanceDAO{db: db}
}

func (d *instanceDAO) Create(ctx context.Context, instance Instance) (int64, error) {
	now := time.Now().UnixMilli()
	instance.Ctime = now
	instance.Utime = now
	if instance.ID == 0 {
		instance.ID = d.db.GetIdGenerator(InstanceCollection)
	}
	_, err := d.db.Collection(InstanceCollection).InsertOne(ctx, instance)
	if err != nil {
		return 0, err
	}
	return instance.ID, nil
}

func (d *instanceDAO) CreateBatch(ctx context.Context, instances []Instance) (int64, error) {
	if len(instances) == 0 {
		return 0, nil
	}
	now := time.Now().UnixMilli()
	docs := make([]interface{}, len(instances))
	for i := range instances {
		if instances[i].ID == 0 {
			instances[i].ID = d.db.GetIdGenerator(InstanceCollection)
		}
		instances[i].Ctime = now
		instances[i].Utime = now
		docs[i] = instances[i]
	}
	result, err := d.db.Collection(InstanceCollection).InsertMany(ctx, docs)
	if err != nil {
		return 0, err
	}
	return int64(len(result.InsertedIDs)), nil
}

func (d *instanceDAO) Update(ctx context.Context, instance Instance) error {
	instance.Utime = time.Now().UnixMilli()
	filter := bson.M{"id": instance.ID}
	update := bson.M{"$set": instance}
	result, err := d.db.Collection(InstanceCollection).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (d *instanceDAO) GetByID(ctx context.Context, id int64) (Instance, error) {
	var instance Instance
	filter := bson.M{"id": id}
	err := d.db.Collection(InstanceCollection).FindOne(ctx, filter).Decode(&instance)
	return instance, err
}

func (d *instanceDAO) GetByAssetID(ctx context.Context, tenantID, modelUID, assetID string) (Instance, error) {
	var instance Instance
	filter := bson.M{"tenant_id": tenantID, "model_uid": modelUID, "asset_id": assetID}
	err := d.db.Collection(InstanceCollection).FindOne(ctx, filter).Decode(&instance)
	return instance, err
}

func (d *instanceDAO) List(ctx context.Context, filter InstanceFilter) ([]Instance, error) {
	query := d.buildQuery(filter)
	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.M{"ctime": -1})

	cursor, err := d.db.Collection(InstanceCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var instances []Instance
	err = cursor.All(ctx, &instances)
	return instances, err
}

func (d *instanceDAO) Count(ctx context.Context, filter InstanceFilter) (int64, error) {
	query := d.buildQuery(filter)
	return d.db.Collection(InstanceCollection).CountDocuments(ctx, query)
}

func (d *instanceDAO) Delete(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := d.db.Collection(InstanceCollection).DeleteOne(ctx, filter)
	return err
}

func (d *instanceDAO) DeleteByAccountID(ctx context.Context, accountID int64) error {
	filter := bson.M{"account_id": accountID}
	_, err := d.db.Collection(InstanceCollection).DeleteMany(ctx, filter)
	return err
}

func (d *instanceDAO) DeleteByAssetIDs(ctx context.Context, tenantID, modelUID string, assetIDs []string) (int64, error) {
	if len(assetIDs) == 0 {
		return 0, nil
	}
	filter := bson.M{"tenant_id": tenantID, "model_uid": modelUID, "asset_id": bson.M{"$in": assetIDs}}
	result, err := d.db.Collection(InstanceCollection).DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

func (d *instanceDAO) ListAssetIDsByRegion(ctx context.Context, tenantID, modelUID string, accountID int64, region string) ([]string, error) {
	filter := bson.M{"tenant_id": tenantID, "model_uid": modelUID, "account_id": accountID, "attributes.region": region}
	opts := options.Find().SetProjection(bson.M{"asset_id": 1})
	cursor, err := d.db.Collection(InstanceCollection).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []struct {
		AssetID string `bson:"asset_id"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	assetIDs := make([]string, len(results))
	for i, r := range results {
		assetIDs[i] = r.AssetID
	}
	return assetIDs, nil
}

func (d *instanceDAO) ListAssetIDsByModelUID(ctx context.Context, tenantID, modelUID string, accountID int64) ([]string, error) {
	filter := bson.M{"tenant_id": tenantID, "model_uid": modelUID, "account_id": accountID}
	opts := options.Find().SetProjection(bson.M{"asset_id": 1})
	cursor, err := d.db.Collection(InstanceCollection).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []struct {
		AssetID string `bson:"asset_id"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	assetIDs := make([]string, len(results))
	for i, r := range results {
		assetIDs[i] = r.AssetID
	}
	return assetIDs, nil
}

func (d *instanceDAO) Upsert(ctx context.Context, instance Instance) error {
	now := time.Now().UnixMilli()
	filter := bson.M{"tenant_id": instance.TenantID, "model_uid": instance.ModelUID, "asset_id": instance.AssetID}
	update := bson.M{
		"$set": bson.M{
			"asset_name": instance.AssetName,
			"account_id": instance.AccountID,
			"attributes": instance.Attributes,
			"utime":      now,
		},
		"$setOnInsert": bson.M{
			"id":        d.db.GetIdGenerator(InstanceCollection),
			"tenant_id": instance.TenantID,
			"model_uid": instance.ModelUID,
			"asset_id":  instance.AssetID,
			"ctime":     now,
		},
	}
	opts := options.Update().SetUpsert(true)
	_, err := d.db.Collection(InstanceCollection).UpdateOne(ctx, filter, update, opts)
	return err
}

func (d *instanceDAO) Search(ctx context.Context, filter SearchFilter) ([]Instance, int64, error) {
	query := d.buildSearchQuery(filter)
	total, err := d.db.Collection(InstanceCollection).CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}

	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	opts.SetLimit(limit)
	opts.SetSort(bson.M{"utime": -1})

	cursor, err := d.db.Collection(InstanceCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var instances []Instance
	err = cursor.All(ctx, &instances)
	return instances, total, err
}

func (d *instanceDAO) buildQuery(filter InstanceFilter) bson.M {
	query := bson.M{}
	if filter.ModelUID != "" {
		switch filter.ModelUID {
		case "cloud_vm", "ecs":
			query["$or"] = []bson.M{{"model_uid": "cloud_vm"}, {"model_uid": bson.M{"$regex": "_ecs$"}}}
		case "cloud_rds", "rds":
			query["$or"] = []bson.M{{"model_uid": "cloud_rds"}, {"model_uid": bson.M{"$regex": "_rds$"}}}
		case "cloud_redis", "redis":
			query["$or"] = []bson.M{{"model_uid": "cloud_redis"}, {"model_uid": bson.M{"$regex": "_redis$"}}}
		case "cloud_mongodb", "mongodb":
			query["$or"] = []bson.M{{"model_uid": "cloud_mongodb"}, {"model_uid": bson.M{"$regex": "_mongodb$"}}}
		case "cloud_vpc", "vpc":
			query["$or"] = []bson.M{{"model_uid": "cloud_vpc"}, {"model_uid": bson.M{"$regex": "_vpc$"}}}
		case "cloud_eip", "eip":
			query["$or"] = []bson.M{{"model_uid": "cloud_eip"}, {"model_uid": bson.M{"$regex": "_eip$"}}}
		case "cloud_nas", "nas":
			query["$or"] = []bson.M{{"model_uid": "cloud_nas"}, {"model_uid": bson.M{"$regex": "_nas$"}}}
		case "cloud_oss", "oss":
			query["$or"] = []bson.M{{"model_uid": "cloud_oss"}, {"model_uid": bson.M{"$regex": "_oss$"}}}
		default:
			query["model_uid"] = filter.ModelUID
		}
	}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.AccountID > 0 {
		query["account_id"] = filter.AccountID
	}
	if filter.AssetID != "" {
		query["asset_id"] = filter.AssetID
	}
	if filter.AssetName != "" {
		query["asset_name"] = bson.M{"$regex": filter.AssetName, "$options": "i"}
	}
	if filter.Provider != "" {
		query["attributes.provider"] = filter.Provider
	}
	if filter.TagFilter != nil {
		if filter.TagFilter.Key != "" {
			if filter.TagFilter.Value != "" {
				query["attributes.tags."+filter.TagFilter.Key] = bson.M{"$regex": filter.TagFilter.Value, "$options": "i"}
			} else {
				query["attributes.tags."+filter.TagFilter.Key] = bson.M{"$exists": true}
			}
		} else if filter.TagFilter.NoTags {
			query["$or"] = []bson.M{{"attributes.tags": bson.M{"$exists": false}}, {"attributes.tags": nil}, {"attributes.tags": bson.M{"$eq": bson.M{}}}}
		} else if filter.TagFilter.HasTags {
			query["attributes.tags"] = bson.M{"$exists": true, "$nin": []interface{}{nil, bson.M{}}}
		}
	}
	for key, value := range filter.Attributes {
		query["attributes."+key] = value
	}
	return query
}

func (d *instanceDAO) buildSearchQuery(filter SearchFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if len(filter.AssetTypes) > 0 {
		var typePatterns []bson.M
		for _, assetType := range filter.AssetTypes {
			switch assetType {
			case "ecs", "cloud_vm":
				typePatterns = append(typePatterns, bson.M{"model_uid": "cloud_vm"}, bson.M{"model_uid": bson.M{"$regex": "_ecs$"}})
			case "rds", "cloud_rds":
				typePatterns = append(typePatterns, bson.M{"model_uid": "cloud_rds"}, bson.M{"model_uid": bson.M{"$regex": "_rds$"}})
			case "redis", "cloud_redis":
				typePatterns = append(typePatterns, bson.M{"model_uid": "cloud_redis"}, bson.M{"model_uid": bson.M{"$regex": "_redis$"}})
			case "mongodb", "cloud_mongodb":
				typePatterns = append(typePatterns, bson.M{"model_uid": "cloud_mongodb"}, bson.M{"model_uid": bson.M{"$regex": "_mongodb$"}})
			case "vpc", "cloud_vpc":
				typePatterns = append(typePatterns, bson.M{"model_uid": "cloud_vpc"}, bson.M{"model_uid": bson.M{"$regex": "_vpc$"}})
			case "eip", "cloud_eip":
				typePatterns = append(typePatterns, bson.M{"model_uid": "cloud_eip"}, bson.M{"model_uid": bson.M{"$regex": "_eip$"}})
			case "nas", "cloud_nas":
				typePatterns = append(typePatterns, bson.M{"model_uid": "cloud_nas"}, bson.M{"model_uid": bson.M{"$regex": "_nas$"}})
			case "oss", "cloud_oss":
				typePatterns = append(typePatterns, bson.M{"model_uid": "cloud_oss"}, bson.M{"model_uid": bson.M{"$regex": "_oss$"}})
			}
		}
		if len(typePatterns) > 0 {
			query["$or"] = typePatterns
		}
	}
	if filter.Provider != "" {
		query["attributes.provider"] = filter.Provider
	}
	if filter.AccountID > 0 {
		query["account_id"] = filter.AccountID
	}
	if filter.Region != "" {
		query["attributes.region"] = filter.Region
	}
	if filter.Keyword != "" {
		keywordQuery := []bson.M{
			{"asset_id": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"asset_name": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"attributes.private_ip": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"attributes.public_ip": bson.M{"$regex": filter.Keyword, "$options": "i"}},
		}
		if existingOr, ok := query["$or"]; ok {
			query["$and"] = []bson.M{{"$or": existingOr}, {"$or": keywordQuery}}
			delete(query, "$or")
		} else {
			query["$or"] = keywordQuery
		}
	}
	return query
}

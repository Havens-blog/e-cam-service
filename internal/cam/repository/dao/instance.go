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
	AssetName  string
	Attributes map[string]interface{}
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
	Upsert(ctx context.Context, instance Instance) error
}

type instanceDAO struct {
	db *mongox.Mongo
}

// NewInstanceDAO 创建实例DAO
func NewInstanceDAO(db *mongox.Mongo) InstanceDAO {
	return &instanceDAO{db: db}
}

// Create 创建单个实例
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

// CreateBatch 批量创建实例
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

// Update 更新实例
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

// GetByID 根据ID获取实例
func (d *instanceDAO) GetByID(ctx context.Context, id int64) (Instance, error) {
	var instance Instance
	filter := bson.M{"id": id}

	err := d.db.Collection(InstanceCollection).FindOne(ctx, filter).Decode(&instance)
	return instance, err
}

// GetByAssetID 根据云厂商资产ID获取实例
func (d *instanceDAO) GetByAssetID(ctx context.Context, tenantID, modelUID, assetID string) (Instance, error) {
	var instance Instance
	filter := bson.M{
		"tenant_id": tenantID,
		"model_uid": modelUID,
		"asset_id":  assetID,
	}

	err := d.db.Collection(InstanceCollection).FindOne(ctx, filter).Decode(&instance)
	return instance, err
}

// List 获取实例列表
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

// Count 统计实例数量
func (d *instanceDAO) Count(ctx context.Context, filter InstanceFilter) (int64, error) {
	query := d.buildQuery(filter)
	return d.db.Collection(InstanceCollection).CountDocuments(ctx, query)
}

// Delete 删除实例
func (d *instanceDAO) Delete(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := d.db.Collection(InstanceCollection).DeleteOne(ctx, filter)
	return err
}

// DeleteByAccountID 删除指定云账号的所有实例
func (d *instanceDAO) DeleteByAccountID(ctx context.Context, accountID int64) error {
	filter := bson.M{"account_id": accountID}
	_, err := d.db.Collection(InstanceCollection).DeleteMany(ctx, filter)
	return err
}

// Upsert 更新或插入实例 (根据 tenant_id + model_uid + asset_id 判断)
func (d *instanceDAO) Upsert(ctx context.Context, instance Instance) error {
	now := time.Now().UnixMilli()
	instance.Utime = now

	filter := bson.M{
		"tenant_id": instance.TenantID,
		"model_uid": instance.ModelUID,
		"asset_id":  instance.AssetID,
	}

	update := bson.M{
		"$set": bson.M{
			"asset_name": instance.AssetName,
			"account_id": instance.AccountID,
			"attributes": instance.Attributes,
			"utime":      now,
		},
		"$setOnInsert": bson.M{
			"id":    d.db.GetIdGenerator(InstanceCollection),
			"ctime": now,
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err := d.db.Collection(InstanceCollection).UpdateOne(ctx, filter, update, opts)
	return err
}

// buildQuery 构建查询条件
func (d *instanceDAO) buildQuery(filter InstanceFilter) bson.M {
	query := bson.M{}

	if filter.ModelUID != "" {
		query["model_uid"] = filter.ModelUID
	}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.AccountID > 0 {
		query["account_id"] = filter.AccountID
	}
	if filter.AssetName != "" {
		query["asset_name"] = bson.M{"$regex": filter.AssetName, "$options": "i"}
	}

	// 动态属性查询
	for key, value := range filter.Attributes {
		query["attributes."+key] = value
	}

	return query
}

// DeleteByAssetIDs 根据 AssetID 列表批量删除实例
func (d *instanceDAO) DeleteByAssetIDs(ctx context.Context, tenantID, modelUID string, assetIDs []string) (int64, error) {
	if len(assetIDs) == 0 {
		return 0, nil
	}

	filter := bson.M{
		"tenant_id": tenantID,
		"model_uid": modelUID,
		"asset_id":  bson.M{"$in": assetIDs},
	}

	result, err := d.db.Collection(InstanceCollection).DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}

	return result.DeletedCount, nil
}

// ListAssetIDsByRegion 获取指定地域的所有 AssetID 列表
func (d *instanceDAO) ListAssetIDsByRegion(ctx context.Context, tenantID, modelUID string, accountID int64, region string) ([]string, error) {
	filter := bson.M{
		"tenant_id":         tenantID,
		"model_uid":         modelUID,
		"account_id":        accountID,
		"attributes.region": region,
	}

	// 只查询 asset_id 字段
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

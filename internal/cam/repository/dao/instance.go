package dao

import (
	"context"
	"strings"
	"time"

	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-common-go/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const InstanceCollection = "ecam_instance"

// Instance DAO层资产实例模型
type Instance struct {
	ID         int64                  `bson:"id"`
	ModelUID   string                 `bson:"model_uid"`
	AssetID    string                 `bson:"asset_id"`
	AssetName  string                 `bson:"asset_name"`
	TenantID   int64                  `bson:"tenant_id"`
	AccountID  int64                  `bson:"account_id"`
	Attributes map[string]interface{} `bson:"attributes"`
	Ctime      int64                  `bson:"ctime"`
	Utime      int64                  `bson:"utime"`
}

// InstanceFilter DAO层过滤条件
type InstanceFilter struct {
	ModelUID   string
	TenantID   int64
	AccountID  int64
	AssetID    string
	AssetName  string
	Provider   string     // 按云平台过滤
	TagFilter  *TagFilter // 标签过滤条件
	Attributes map[string]interface{}
	Offset     int64
	Limit      int64
}

// TagFilter 标签过滤条件
type TagFilter struct {
	HasTags bool   // 过滤有标签的实例
	NoTags  bool   // 过滤没有标签的实例
	Key     string // 标签键
	Value   string // 标签值
}

// InstanceDAO 资产实例数据访问接口
type InstanceDAO interface {
	Create(ctx context.Context, instance Instance) (int64, error)
	CreateBatch(ctx context.Context, instances []Instance) (int64, error)
	Update(ctx context.Context, instance Instance) error
	GetByID(ctx context.Context, id int64) (Instance, error)
	GetByAssetID(ctx context.Context, tenantID int64, modelUID, assetID string) (Instance, error)
	List(ctx context.Context, filter InstanceFilter) ([]Instance, error)
	Count(ctx context.Context, filter InstanceFilter) (int64, error)
	Delete(ctx context.Context, id int64) error
	DeleteByAccountID(ctx context.Context, accountID int64) error
	DeleteByAssetIDs(ctx context.Context, tenantID int64, modelUID string, assetIDs []string) (int64, error)
	ListAssetIDsByRegion(ctx context.Context, tenantID int64, modelUID string, accountID int64, region string) ([]string, error)
	ListAssetIDsByModelUID(ctx context.Context, tenantID int64, modelUID string, accountID int64) ([]string, error)
	// DistinctAttribute 返回租户下某属性点路径的去重字符串值（规则条件枚举专用）。
	DistinctAttribute(ctx context.Context, tenantID int64, path string) ([]string, error)
	Upsert(ctx context.Context, instance Instance) error
	Search(ctx context.Context, filter SearchFilter) ([]Instance, int64, error)
}

// SearchFilter 统一搜索过滤条件
type SearchFilter struct {
	TenantID   int64    // 租户ID (必填)
	Keyword    string   // 搜索关键词
	AssetTypes []string // 资产类型列表
	Provider   string   // 云厂商过滤
	AccountID  int64    // 云账号过滤
	Region     string   // 地域过滤
	Offset     int64
	Limit      int64
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
func (d *instanceDAO) GetByAssetID(ctx context.Context, tenantID int64, modelUID, assetID string) (Instance, error) {
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

// DistinctAttribute 返回租户下某属性点路径的去重字符串值。
// 走 Mongo distinct 聚合，避免规则枚举时把全量资产文档拉回内存（2w+ 资产时 ListFieldValues 曾 4s+）。
// 仅保留字符串类型且非空的值（与 getFieldValue 取值口径一致）。
func (d *instanceDAO) DistinctAttribute(ctx context.Context, tenantID int64, path string) ([]string, error) {
	vals, err := d.db.Collection(InstanceCollection).Distinct(ctx, path, bson.M{"tenant_id": tenantID})
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(vals))
	for _, v := range vals {
		if s, ok := v.(string); ok && s != "" {
			result = append(result, s)
		}
	}
	return result, nil
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

	filter := bson.M{
		"tenant_id": instance.TenantID,
		"model_uid": instance.ModelUID,
		"asset_id":  instance.AssetID,
	}

	// 更新所有可变字段
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

// buildQuery 构建查询条件
func (d *instanceDAO) buildQuery(filter InstanceFilter) bson.M {
	query := bson.M{}

	if filter.ModelUID != "" {
		// 支持通用资产类型查询（注册表收敛：domain.ModelUIDPatternFor）
		if pat, ok := domain.ModelUIDPatternFor(filter.ModelUID); ok {
			query["$or"] = modelUIDOrPatterns(pat)
		} else {
			query["model_uid"] = filter.ModelUID
		}
	}
	if filter.TenantID != 0 {
		query["tenant_id"] = filter.TenantID
	}
	if filter.AccountID > 0 {
		query["account_id"] = filter.AccountID
	}
	if filter.AssetID != "" {
		query["asset_id"] = filter.AssetID
	}
	if filter.AssetName != "" {
		// 模糊搜索：同时匹配 asset_name 和 asset_id
		nameOr := []bson.M{
			{"asset_name": bson.M{"$regex": filter.AssetName, "$options": "i"}},
			{"asset_id": bson.M{"$regex": filter.AssetName, "$options": "i"}},
		}
		// 如果已有 $or（来自 model_uid 匹配），用 $and 组合
		if existing, ok := query["$or"]; ok {
			query["$and"] = []bson.M{
				{"$or": existing.([]bson.M)},
				{"$or": nameOr},
			}
			delete(query, "$or")
		} else {
			query["$or"] = nameOr
		}
	}
	// 按云平台过滤 (查询 attributes.provider)
	if filter.Provider != "" {
		query["attributes.provider"] = filter.Provider
	}

	// 标签过滤
	if filter.TagFilter != nil {
		if filter.TagFilter.Key != "" {
			// 按标签键过滤 (优先处理，因为更精确)
			if filter.TagFilter.Value != "" {
				// 按标签键值对过滤 (标签值支持模糊匹配)
				query["attributes.tags."+filter.TagFilter.Key] = bson.M{
					"$regex":   filter.TagFilter.Value,
					"$options": "i",
				}
			} else {
				// 只按标签键过滤 (存在此键)
				query["attributes.tags."+filter.TagFilter.Key] = bson.M{"$exists": true}
			}
		} else if filter.TagFilter.NoTags {
			// 没有标签: tags 不存在、为null、或为空对象
			// 使用 $where 来检查空对象 (兼容性更好)
			query["$or"] = []bson.M{
				{"attributes.tags": bson.M{"$exists": false}},
				{"attributes.tags": nil},
				{"attributes.tags": bson.M{"$eq": bson.M{}}},
			}
		} else if filter.TagFilter.HasTags {
			// 有标签: tags 存在、不为null、且不为空对象
			// 使用 $ne 排除空对象
			query["attributes.tags"] = bson.M{
				"$exists": true,
				"$nin":    []interface{}{nil, bson.M{}},
			}
		}
	}

	// 动态属性查询
	for key, value := range filter.Attributes {
		query["attributes."+key] = value
	}

	return query
}

// DeleteByAssetIDs 根据 AssetID 列表批量删除实例
func (d *instanceDAO) DeleteByAssetIDs(ctx context.Context, tenantID int64, modelUID string, assetIDs []string) (int64, error) {
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
func (d *instanceDAO) ListAssetIDsByRegion(ctx context.Context, tenantID int64, modelUID string, accountID int64, region string) ([]string, error) {
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

// ListAssetIDsByModelUID 获取指定模型的所有 AssetID 列表（不按地域过滤，用于 OSS 等全局资源）
func (d *instanceDAO) ListAssetIDsByModelUID(ctx context.Context, tenantID int64, modelUID string, accountID int64) ([]string, error) {
	filter := bson.M{
		"tenant_id":  tenantID,
		"model_uid":  modelUID,
		"account_id": accountID,
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

// Search 统一搜索实例
// 支持按关键词搜索 asset_id, asset_name, ip 地址等。
// 关键词命中时按相关性排序（精确ID > 精确名 > ID前缀 > 名前缀 > ID包含 > 名包含 > 其它字段），
// 同分按 utime 倒序；避免短关键词下目标实例被单纯按更新时间倒序挤出前 N（截断发生在相关性排序之后）。
func (d *instanceDAO) Search(ctx context.Context, filter SearchFilter) ([]Instance, int64, error) {
	query := d.buildSearchQuery(filter)

	// 统计总数
	total, err := d.db.Collection(InstanceCollection).CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}

	pipeline := mongo.Pipeline{bson.D{{Key: "$match", Value: query}}}

	// 相关性排序：无关键词时保持原 utime 倒序
	if filter.Keyword != "" {
		pipeline = append(pipeline, bson.D{{Key: "$addFields", Value: bson.D{
			{Key: "_score", Value: relevanceScoreExpr(filter.Keyword)},
		}}})
		pipeline = append(pipeline, bson.D{{Key: "$sort", Value: bson.D{
			{Key: "_score", Value: 1},
			{Key: "utime", Value: -1},
		}}})
	} else {
		pipeline = append(pipeline, bson.D{{Key: "$sort", Value: bson.D{{Key: "utime", Value: -1}}}})
	}

	if filter.Offset > 0 {
		pipeline = append(pipeline, bson.D{{Key: "$skip", Value: filter.Offset}})
	}
	pipeline = append(pipeline, bson.D{{Key: "$limit", Value: limit}})

	cursor, err := d.db.Collection(InstanceCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var instances []Instance
	err = cursor.All(ctx, &instances)
	return instances, total, err
}

// relevanceScoreExpr 构建关键词相关性评分表达式（$switch）。
// 分值越小越靠前：精确ID(0) < 精确名(1) < ID前缀(2) < 名前缀(3) < ID包含(4) < 名包含(5) < 其它字段命中(6)。
// 用 $toLower + $indexOfCP 实现大小写不敏感的等值/前缀/包含判定，避免正则转义。
func relevanceScoreExpr(keyword string) bson.D {
	kw := strings.ToLower(keyword)

	// lower：字段统一转小写（asset_id/asset_name 恒为字符串，安全）
	lower := func(field string) bson.D {
		return bson.D{{Key: "$toLower", Value: field}}
	}
	eq := func(field string) bson.D {
		return bson.D{{Key: "$eq", Value: bson.A{lower(field), kw}}}
	}
	prefix := func(field string) bson.D {
		return bson.D{{Key: "$eq", Value: bson.A{
			bson.D{{Key: "$indexOfCP", Value: bson.A{lower(field), kw}}},
			0,
		}}}
	}
	contains := func(field string) bson.D {
		return bson.D{{Key: "$ne", Value: bson.A{
			bson.D{{Key: "$indexOfCP", Value: bson.A{lower(field), kw}}},
			-1,
		}}}
	}
	branch := func(score int, cond bson.D) bson.D {
		return bson.D{{Key: "case", Value: cond}, {Key: "then", Value: score}}
	}

	return bson.D{{Key: "$switch", Value: bson.D{
		{Key: "branches", Value: bson.A{
			branch(0, eq("$asset_id")),
			branch(1, eq("$asset_name")),
			branch(2, prefix("$asset_id")),
			branch(3, prefix("$asset_name")),
			branch(4, contains("$asset_id")),
			branch(5, contains("$asset_name")),
		}},
		{Key: "default", Value: 6},
	}}}
}

// buildSearchQuery 构建搜索查询条件
// modelUIDOrPatterns 资源类型匹配模式 → bson $or 条件（通用 model_uid 精确匹配 + 厂商后缀正则）。
func modelUIDOrPatterns(pat domain.ModelUIDPattern) []bson.M {
	ors := make([]bson.M, 0, len(pat.Generics)+len(pat.Suffixes))
	for _, g := range pat.Generics {
		ors = append(ors, bson.M{"model_uid": g})
	}
	for _, s := range pat.Suffixes {
		ors = append(ors, bson.M{"model_uid": bson.M{"$regex": "_" + s + "$"}})
	}
	return ors
}

func (d *instanceDAO) buildSearchQuery(filter SearchFilter) bson.M {
	query := bson.M{}

	// 租户ID (必填)
	if filter.TenantID != 0 {
		query["tenant_id"] = filter.TenantID
	}

	// 资产类型过滤
	if len(filter.AssetTypes) > 0 {
		// 构建正则表达式匹配多种资产类型
		// 同时匹配通用模型 (cloud_vm) 和云厂商模型 (*_ecs)
		var typePatterns []bson.M
		for _, assetType := range filter.AssetTypes {
			// 注册表收敛（含 cdn/waf/eni，修复与 buildQuery 的清单漂移）
			if pat, ok := domain.ModelUIDPatternFor(assetType); ok {
				typePatterns = append(typePatterns, modelUIDOrPatterns(pat)...)
			}
		}
		if len(typePatterns) > 0 {
			query["$or"] = typePatterns
		}
	}

	// 云厂商过滤
	if filter.Provider != "" {
		query["attributes.provider"] = filter.Provider
	}

	// 云账号过滤
	if filter.AccountID > 0 {
		query["account_id"] = filter.AccountID
	}

	// 地域过滤
	if filter.Region != "" {
		query["attributes.region"] = filter.Region
	}

	// 关键词搜索 (模糊匹配多个字段)
	if filter.Keyword != "" {
		keywordQuery := []bson.M{
			{"asset_id": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"asset_name": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"attributes.private_ip": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"attributes.public_ip": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"attributes.ip_address": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"attributes.address": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"attributes.connection_string": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"attributes.cidr_block": bson.M{"$regex": filter.Keyword, "$options": "i"}},
		}

		// 如果已有 $or 条件 (资产类型)，需要用 $and 组合
		if existingOr, ok := query["$or"]; ok {
			query["$and"] = []bson.M{
				{"$or": existingOr},
				{"$or": keywordQuery},
			}
			delete(query, "$or")
		} else {
			query["$or"] = keywordQuery
		}
	}

	return query
}

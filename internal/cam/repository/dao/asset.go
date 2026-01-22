package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const AssetCollection = "cloud_assets"

// Asset DAO层资产模型
type Asset struct {
	Id           int64     `bson:"id"`
	AssetId      string    `bson:"asset_id"`
	AssetName    string    `bson:"asset_name"`
	AssetType    string    `bson:"asset_type"`
	Provider     string    `bson:"provider"`
	Region       string    `bson:"region"`
	Zone         string    `bson:"zone"`
	Status       string    `bson:"status"`
	Tags         []Tag     `bson:"tags"`
	Metadata     string    `bson:"metadata"`
	Cost         float64   `bson:"cost"`
	CreateTime   time.Time `bson:"create_time"`
	UpdateTime   time.Time `bson:"update_time"`
	DiscoverTime time.Time `bson:"discover_time"`
	Ctime        int64     `bson:"ctime"`
	Utime        int64     `bson:"utime"`
}

// Tag 标签
type Tag struct {
	Key   string `bson:"key" json:"key"`
	Value string `bson:"value" json:"value"`
}

// AssetDAO 资产数据访问接口
type AssetDAO interface {
	CreateAsset(ctx context.Context, asset Asset) (int64, error)
	CreateMultiAssets(ctx context.Context, assets []Asset) (int64, error)
	UpdateAsset(ctx context.Context, asset Asset) error
	GetAssetById(ctx context.Context, id int64) (Asset, error)
	GetAssetByAssetId(ctx context.Context, assetId string) (Asset, error)
	ListAssets(ctx context.Context, filter AssetFilter) ([]Asset, error)
	CountAssets(ctx context.Context, filter AssetFilter) (int64, error)
	DeleteAsset(ctx context.Context, id int64) error
}

// AssetFilter DAO层过滤条件
type AssetFilter struct {
	Provider  string
	AssetType string
	Region    string
	Status    string
	AssetName string
	Offset    int64
	Limit     int64
}

type assetDAO struct {
	db *mongox.Mongo
}

// NewAssetDAO 创建资产DAO
func NewAssetDAO(db *mongox.Mongo) AssetDAO {
	return &assetDAO{
		db: db,
	}
}

// CreateAsset 创建单个资产
func (dao *assetDAO) CreateAsset(ctx context.Context, asset Asset) (int64, error) {
	now := time.Now().Unix()
	asset.Ctime = now
	asset.Utime = now

	if asset.Id == 0 {
		asset.Id = dao.db.GetIdGenerator(AssetCollection)
	}

	_, err := dao.db.Collection(AssetCollection).InsertOne(ctx, asset)
	if err != nil {
		return 0, err
	}

	return asset.Id, nil
}

// CreateMultiAssets 批量创建资产
func (dao *assetDAO) CreateMultiAssets(ctx context.Context, assets []Asset) (int64, error) {
	if len(assets) == 0 {
		return 0, nil
	}

	now := time.Now().Unix()
	docs := make([]interface{}, len(assets))

	for i, asset := range assets {
		if asset.Id == 0 {
			asset.Id = dao.db.GetIdGenerator(AssetCollection)
		}
		asset.Ctime = now
		asset.Utime = now
		docs[i] = asset
	}

	result, err := dao.db.Collection(AssetCollection).InsertMany(ctx, docs)
	if err != nil {
		return 0, err
	}

	return int64(len(result.InsertedIDs)), nil
}

// UpdateAsset 更新资产
func (dao *assetDAO) UpdateAsset(ctx context.Context, asset Asset) error {
	asset.Utime = time.Now().Unix()

	filter := bson.M{"id": asset.Id}
	update := bson.M{"$set": asset}

	_, err := dao.db.Collection(AssetCollection).UpdateOne(ctx, filter, update)
	return err
}

// GetAssetById 根据ID获取资产
func (dao *assetDAO) GetAssetById(ctx context.Context, id int64) (Asset, error) {
	var asset Asset
	filter := bson.M{"id": id}

	err := dao.db.Collection(AssetCollection).FindOne(ctx, filter).Decode(&asset)
	return asset, err
}

// GetAssetByAssetId 根据资产ID获取资产
func (dao *assetDAO) GetAssetByAssetId(ctx context.Context, assetId string) (Asset, error) {
	var asset Asset
	filter := bson.M{"asset_id": assetId}

	err := dao.db.Collection(AssetCollection).FindOne(ctx, filter).Decode(&asset)
	return asset, err
}

// ListAssets 获取资产列表
func (dao *assetDAO) ListAssets(ctx context.Context, filter AssetFilter) ([]Asset, error) {
	var assets []Asset

	// 构建查询条件
	query := bson.M{}
	if filter.Provider != "" {
		query["provider"] = filter.Provider
	}
	if filter.AssetType != "" {
		query["asset_type"] = filter.AssetType
	}
	if filter.Region != "" {
		query["region"] = filter.Region
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.AssetName != "" {
		query["asset_name"] = bson.M{"$regex": filter.AssetName, "$options": "i"}
	}

	// 设置分页选项
	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.M{"ctime": -1})

	cursor, err := dao.db.Collection(AssetCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	err = cursor.All(ctx, &assets)
	return assets, err
}

// CountAssets 统计资产数量
func (dao *assetDAO) CountAssets(ctx context.Context, filter AssetFilter) (int64, error) {
	// 构建查询条件
	query := bson.M{}
	if filter.Provider != "" {
		query["provider"] = filter.Provider
	}
	if filter.AssetType != "" {
		query["asset_type"] = filter.AssetType
	}
	if filter.Region != "" {
		query["region"] = filter.Region
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.AssetName != "" {
		query["asset_name"] = bson.M{"$regex": filter.AssetName, "$options": "i"}
	}

	return dao.db.Collection(AssetCollection).CountDocuments(ctx, query)
}

// DeleteAsset 删除资产
func (dao *assetDAO) DeleteAsset(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := dao.db.Collection(AssetCollection).DeleteOne(ctx, filter)
	return err
}

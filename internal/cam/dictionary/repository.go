package dictionary

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	DictTypeCollection = "ecam_dict_type"
	DictItemCollection = "ecam_dict_item"
)

// DictDAO 数据字典数据访问接口
type DictDAO interface {
	// 字典类型
	InsertType(ctx context.Context, dt DictType) (int64, error)
	UpdateType(ctx context.Context, dt DictType) error
	DeleteType(ctx context.Context, id int64) error
	GetTypeByID(ctx context.Context, id int64) (DictType, error)
	GetTypeByCode(ctx context.Context, tenantID, code string) (DictType, error)
	ListTypes(ctx context.Context, filter TypeFilter) ([]DictType, int64, error)
	UpdateTypeStatus(ctx context.Context, id int64, status string) error

	// 字典项
	InsertItem(ctx context.Context, item DictItem) (int64, error)
	UpdateItem(ctx context.Context, item DictItem) error
	DeleteItem(ctx context.Context, id int64) error
	GetItemByID(ctx context.Context, id int64) (DictItem, error)
	GetItemByValue(ctx context.Context, typeID int64, value string) (DictItem, error)
	ListItemsByTypeID(ctx context.Context, typeID int64) ([]DictItem, error)
	ListEnabledItemsByTypeID(ctx context.Context, typeID int64) ([]DictItem, error)
	CountItemsByTypeID(ctx context.Context, typeID int64) (int64, error)
	UpdateItemStatus(ctx context.Context, id int64, status string) error
}

type dictDAO struct {
	db *mongox.Mongo
}

// NewDictDAO 创建数据字典 DAO
func NewDictDAO(db *mongox.Mongo) DictDAO {
	return &dictDAO{db: db}
}

// ==================== 字典类型操作 ====================

func (d *dictDAO) InsertType(ctx context.Context, dt DictType) (int64, error) {
	now := time.Now().UnixMilli()
	dt.Ctime = now
	dt.Utime = now
	if dt.ID == 0 {
		dt.ID = d.db.GetIdGenerator(DictTypeCollection)
	}
	_, err := d.db.Collection(DictTypeCollection).InsertOne(ctx, dt)
	if err != nil {
		return 0, err
	}
	return dt.ID, nil
}

func (d *dictDAO) UpdateType(ctx context.Context, dt DictType) error {
	filter := bson.M{"id": dt.ID, "tenant_id": dt.TenantID}
	update := bson.M{
		"$set": bson.M{
			"name":        dt.Name,
			"description": dt.Description,
			"utime":       time.Now().UnixMilli(),
		},
	}
	_, err := d.db.Collection(DictTypeCollection).UpdateOne(ctx, filter, update)
	return err
}

func (d *dictDAO) DeleteType(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := d.db.Collection(DictTypeCollection).DeleteOne(ctx, filter)
	return err
}

func (d *dictDAO) GetTypeByID(ctx context.Context, id int64) (DictType, error) {
	var dt DictType
	filter := bson.M{"id": id}
	err := d.db.Collection(DictTypeCollection).FindOne(ctx, filter).Decode(&dt)
	return dt, err
}

func (d *dictDAO) GetTypeByCode(ctx context.Context, tenantID, code string) (DictType, error) {
	var dt DictType
	filter := bson.M{"tenant_id": tenantID, "code": code}
	err := d.db.Collection(DictTypeCollection).FindOne(ctx, filter).Decode(&dt)
	return dt, err
}

func (d *dictDAO) ListTypes(ctx context.Context, filter TypeFilter) ([]DictType, int64, error) {
	query := bson.M{"tenant_id": filter.TenantID}
	if filter.Keyword != "" {
		regex := primitive.Regex{Pattern: filter.Keyword, Options: "i"}
		query["$or"] = bson.A{
			bson.M{"name": regex},
			bson.M{"code": regex},
		}
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}

	total, err := d.db.Collection(DictTypeCollection).CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}

	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "ctime", Value: -1}})

	cursor, err := d.db.Collection(DictTypeCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var types []DictType
	if err = cursor.All(ctx, &types); err != nil {
		return nil, 0, err
	}
	return types, total, nil
}

func (d *dictDAO) UpdateTypeStatus(ctx context.Context, id int64, status string) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"status": status,
			"utime":  time.Now().UnixMilli(),
		},
	}
	_, err := d.db.Collection(DictTypeCollection).UpdateOne(ctx, filter, update)
	return err
}

// ==================== 字典项操作 ====================

func (d *dictDAO) InsertItem(ctx context.Context, item DictItem) (int64, error) {
	now := time.Now().UnixMilli()
	item.Ctime = now
	item.Utime = now
	if item.ID == 0 {
		item.ID = d.db.GetIdGenerator(DictItemCollection)
	}
	_, err := d.db.Collection(DictItemCollection).InsertOne(ctx, item)
	if err != nil {
		return 0, err
	}
	return item.ID, nil
}

func (d *dictDAO) UpdateItem(ctx context.Context, item DictItem) error {
	filter := bson.M{"id": item.ID}
	update := bson.M{
		"$set": bson.M{
			"label":      item.Label,
			"sort_order": item.SortOrder,
			"status":     item.Status,
			"extra":      item.Extra,
			"utime":      time.Now().UnixMilli(),
		},
	}
	_, err := d.db.Collection(DictItemCollection).UpdateOne(ctx, filter, update)
	return err
}

func (d *dictDAO) DeleteItem(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := d.db.Collection(DictItemCollection).DeleteOne(ctx, filter)
	return err
}

func (d *dictDAO) GetItemByID(ctx context.Context, id int64) (DictItem, error) {
	var item DictItem
	filter := bson.M{"id": id}
	err := d.db.Collection(DictItemCollection).FindOne(ctx, filter).Decode(&item)
	return item, err
}

func (d *dictDAO) GetItemByValue(ctx context.Context, typeID int64, value string) (DictItem, error) {
	var item DictItem
	filter := bson.M{"dict_type_id": typeID, "value": value}
	err := d.db.Collection(DictItemCollection).FindOne(ctx, filter).Decode(&item)
	return item, err
}

func (d *dictDAO) ListItemsByTypeID(ctx context.Context, typeID int64) ([]DictItem, error) {
	filter := bson.M{"dict_type_id": typeID}
	opts := options.Find().SetSort(bson.D{{Key: "sort_order", Value: 1}})

	cursor, err := d.db.Collection(DictItemCollection).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var items []DictItem
	err = cursor.All(ctx, &items)
	return items, err
}

func (d *dictDAO) ListEnabledItemsByTypeID(ctx context.Context, typeID int64) ([]DictItem, error) {
	filter := bson.M{"dict_type_id": typeID, "status": "enabled"}
	opts := options.Find().SetSort(bson.D{{Key: "sort_order", Value: 1}})

	cursor, err := d.db.Collection(DictItemCollection).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var items []DictItem
	err = cursor.All(ctx, &items)
	return items, err
}

func (d *dictDAO) CountItemsByTypeID(ctx context.Context, typeID int64) (int64, error) {
	filter := bson.M{"dict_type_id": typeID}
	return d.db.Collection(DictItemCollection).CountDocuments(ctx, filter)
}

func (d *dictDAO) UpdateItemStatus(ctx context.Context, id int64, status string) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"status": status,
			"utime":  time.Now().UnixMilli(),
		},
	}
	_, err := d.db.Collection(DictItemCollection).UpdateOne(ctx, filter, update)
	return err
}

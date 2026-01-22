package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const FieldCollection = "c_attribute"

// ModelField DAO层字段模型
type ModelField struct {
	ID          int64  `bson:"id"`
	FieldUID    string `bson:"field_uid"`
	FieldName   string `bson:"field_name"`
	FieldType   string `bson:"field_type"`
	ModelUID    string `bson:"model_uid"`
	GroupID     int64  `bson:"group_id"`
	DisplayName string `bson:"display_name"` // 显示名称
	Display     bool   `bson:"display"`      // 是否显示（兼容旧系统）
	Index       int    `bson:"index"`
	Required    bool   `bson:"required"`
	Secure      bool   `bson:"secure"`
	Link        bool   `bson:"link"`       // 是否为关联字段（兼容旧系统）
	LinkModel   string `bson:"link_model"` // 关联模型UID
	Option      string `bson:"option"`
	Ctime       int64  `bson:"ctime"`
	Utime       int64  `bson:"utime"`
}

// ModelFieldFilter 字段过滤条件
type ModelFieldFilter struct {
	ModelUID  string
	GroupID   int64
	FieldType string
	Required  *bool
	Secure    *bool
	Offset    int
	Limit     int
}

// ModelFieldDAO 字段数据访问接口
type ModelFieldDAO interface {
	// CreateField 创建字段
	CreateField(ctx context.Context, field ModelField) (int64, error)

	// GetFieldByUID 根据UID获取字段
	GetFieldByUID(ctx context.Context, fieldUID string) (ModelField, error)

	// GetFieldByID 根据ID获取字段
	GetFieldByID(ctx context.Context, id int64) (ModelField, error)

	// ListFields 获取字段列表
	ListFields(ctx context.Context, filter ModelFieldFilter) ([]ModelField, error)

	// GetFieldsByModelUID 获取模型的所有字段
	GetFieldsByModelUID(ctx context.Context, modelUID string) ([]ModelField, error)

	// GetFieldsByGroupID 获取分组的所有字段
	GetFieldsByGroupID(ctx context.Context, groupID int64) ([]ModelField, error)

	// UpdateField 更新字段
	UpdateField(ctx context.Context, field ModelField) error

	// DeleteField 删除字段
	DeleteField(ctx context.Context, fieldUID string) error

	// DeleteFieldsByModelUID 删除模型的所有字段
	DeleteFieldsByModelUID(ctx context.Context, modelUID string) error

	// FieldExists 检查字段是否存在
	FieldExists(ctx context.Context, fieldUID string) (bool, error)
}

type modelFieldDAO struct {
	db *mongox.Mongo
}

// NewModelFieldDAO 创建字段DAO
func NewModelFieldDAO(db *mongox.Mongo) ModelFieldDAO {
	return &modelFieldDAO{db: db}
}

// CreateField 创建字段
func (d *modelFieldDAO) CreateField(ctx context.Context, field ModelField) (int64, error) {
	now := time.Now().UnixMilli()
	field.Ctime = now
	field.Utime = now

	// 生成业务ID
	if field.ID == 0 {
		field.ID = d.db.GetIdGenerator(FieldCollection)
	}

	col := d.db.Collection(FieldCollection)
	_, err := col.InsertOne(ctx, field)
	if err != nil {
		return 0, err
	}

	return field.ID, nil
}

// GetFieldByUID 根据UID获取字段
func (d *modelFieldDAO) GetFieldByUID(ctx context.Context, fieldUID string) (ModelField, error) {
	var field ModelField
	col := d.db.Collection(FieldCollection)

	filter := bson.M{"field_uid": fieldUID}
	err := col.FindOne(ctx, filter).Decode(&field)
	if err != nil {
		return ModelField{}, err
	}

	return field, nil
}

// GetFieldByID 根据ID获取字段
func (d *modelFieldDAO) GetFieldByID(ctx context.Context, id int64) (ModelField, error) {
	var field ModelField
	col := d.db.Collection(FieldCollection)

	filter := bson.M{"id": id}
	err := col.FindOne(ctx, filter).Decode(&field)
	if err != nil {
		return ModelField{}, err
	}

	return field, nil
}

// ListFields 获取字段列表
func (d *modelFieldDAO) ListFields(ctx context.Context, filter ModelFieldFilter) ([]ModelField, error) {
	col := d.db.Collection(FieldCollection)

	// 构建查询条件
	query := bson.M{}
	if filter.ModelUID != "" {
		query["model_uid"] = filter.ModelUID
	}
	if filter.GroupID > 0 {
		query["group_id"] = filter.GroupID
	}
	if filter.FieldType != "" {
		query["field_type"] = filter.FieldType
	}
	if filter.Required != nil {
		query["required"] = *filter.Required
	}
	if filter.Secure != nil {
		query["secure"] = *filter.Secure
	}

	// 设置分页和排序
	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(int64(filter.Offset))
	}
	if filter.Limit > 0 {
		opts.SetLimit(int64(filter.Limit))
	}
	opts.SetSort(bson.D{{Key: "index", Value: 1}, {Key: "id", Value: 1}})

	cursor, err := col.Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var fields []ModelField
	if err = cursor.All(ctx, &fields); err != nil {
		return nil, err
	}

	return fields, nil
}

// GetFieldsByModelUID 获取模型的所有字段
func (d *modelFieldDAO) GetFieldsByModelUID(ctx context.Context, modelUID string) ([]ModelField, error) {
	return d.ListFields(ctx, ModelFieldFilter{ModelUID: modelUID})
}

// GetFieldsByGroupID 获取分组的所有字段
func (d *modelFieldDAO) GetFieldsByGroupID(ctx context.Context, groupID int64) ([]ModelField, error) {
	return d.ListFields(ctx, ModelFieldFilter{GroupID: groupID})
}

// UpdateField 更新字段
func (d *modelFieldDAO) UpdateField(ctx context.Context, field ModelField) error {
	col := d.db.Collection(FieldCollection)

	field.Utime = time.Now().UnixMilli()

	filter := bson.M{"field_uid": field.FieldUID}
	update := bson.M{
		"$set": bson.M{
			"field_name":   field.FieldName,
			"field_type":   field.FieldType,
			"group_id":     field.GroupID,
			"display_name": field.DisplayName,
			"display":      field.Display,
			"index":        field.Index,
			"required":     field.Required,
			"secure":       field.Secure,
			"link":         field.Link,
			"link_model":   field.LinkModel,
			"option":       field.Option,
			"utime":        field.Utime,
		},
	}

	result, err := col.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}

	return nil
}

// DeleteField 删除字段
func (d *modelFieldDAO) DeleteField(ctx context.Context, fieldUID string) error {
	col := d.db.Collection(FieldCollection)

	filter := bson.M{"field_uid": fieldUID}
	result, err := col.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}

	if result.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}

	return nil
}

// DeleteFieldsByModelUID 删除模型的所有字段
func (d *modelFieldDAO) DeleteFieldsByModelUID(ctx context.Context, modelUID string) error {
	col := d.db.Collection(FieldCollection)

	filter := bson.M{"model_uid": modelUID}
	_, err := col.DeleteMany(ctx, filter)
	return err
}

// FieldExists 检查字段是否存在
func (d *modelFieldDAO) FieldExists(ctx context.Context, fieldUID string) (bool, error) {
	col := d.db.Collection(FieldCollection)

	filter := bson.M{"field_uid": fieldUID}
	count, err := col.CountDocuments(ctx, filter)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

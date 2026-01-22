package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const FieldGroupCollection = "c_attribute_group"

// ModelFieldGroup DAO层字段分组模型
type ModelFieldGroup struct {
	ID       int64  `bson:"id"`
	ModelUID string `bson:"model_uid"`
	Name     string `bson:"name"`
	Index    int    `bson:"index"`
	Ctime    int64  `bson:"ctime"`
	Utime    int64  `bson:"utime"`
}

// ModelFieldGroupFilter 分组过滤条件
type ModelFieldGroupFilter struct {
	ModelUID string
	Offset   int
	Limit    int
}

// ModelFieldGroupDAO 字段分组数据访问接口
type ModelFieldGroupDAO interface {
	// CreateGroup 创建分组
	CreateGroup(ctx context.Context, group ModelFieldGroup) (int64, error)

	// GetGroupByID 根据ID获取分组
	GetGroupByID(ctx context.Context, id int64) (ModelFieldGroup, error)

	// ListGroups 获取分组列表
	ListGroups(ctx context.Context, filter ModelFieldGroupFilter) ([]ModelFieldGroup, error)

	// GetGroupsByModelUID 获取模型的所有分组
	GetGroupsByModelUID(ctx context.Context, modelUID string) ([]ModelFieldGroup, error)

	// UpdateGroup 更新分组
	UpdateGroup(ctx context.Context, group ModelFieldGroup) error

	// DeleteGroup 删除分组
	DeleteGroup(ctx context.Context, id int64) error

	// DeleteGroupsByModelUID 删除模型的所有分组
	DeleteGroupsByModelUID(ctx context.Context, modelUID string) error
}

type modelFieldGroupDAO struct {
	db *mongox.Mongo
}

// NewModelFieldGroupDAO 创建字段分组DAO
func NewModelFieldGroupDAO(db *mongox.Mongo) ModelFieldGroupDAO {
	return &modelFieldGroupDAO{db: db}
}

// CreateGroup 创建分组
func (d *modelFieldGroupDAO) CreateGroup(ctx context.Context, group ModelFieldGroup) (int64, error) {
	now := time.Now().UnixMilli()
	group.Ctime = now
	group.Utime = now

	// 生成业务ID
	if group.ID == 0 {
		group.ID = d.db.GetIdGenerator(FieldGroupCollection)
	}

	col := d.db.Collection(FieldGroupCollection)
	_, err := col.InsertOne(ctx, group)
	if err != nil {
		return 0, err
	}

	return group.ID, nil
}

// GetGroupByID 根据ID获取分组
func (d *modelFieldGroupDAO) GetGroupByID(ctx context.Context, id int64) (ModelFieldGroup, error) {
	var group ModelFieldGroup
	col := d.db.Collection(FieldGroupCollection)

	filter := bson.M{"id": id}
	err := col.FindOne(ctx, filter).Decode(&group)
	if err != nil {
		return ModelFieldGroup{}, err
	}

	return group, nil
}

// ListGroups 获取分组列表
func (d *modelFieldGroupDAO) ListGroups(ctx context.Context, filter ModelFieldGroupFilter) ([]ModelFieldGroup, error) {
	col := d.db.Collection(FieldGroupCollection)

	// 构建查询条件
	query := bson.M{}
	if filter.ModelUID != "" {
		query["model_uid"] = filter.ModelUID
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

	var groups []ModelFieldGroup
	if err = cursor.All(ctx, &groups); err != nil {
		return nil, err
	}

	return groups, nil
}

// GetGroupsByModelUID 获取模型的所有分组
func (d *modelFieldGroupDAO) GetGroupsByModelUID(ctx context.Context, modelUID string) ([]ModelFieldGroup, error) {
	return d.ListGroups(ctx, ModelFieldGroupFilter{ModelUID: modelUID})
}

// UpdateGroup 更新分组
func (d *modelFieldGroupDAO) UpdateGroup(ctx context.Context, group ModelFieldGroup) error {
	col := d.db.Collection(FieldGroupCollection)

	group.Utime = time.Now().UnixMilli()

	filter := bson.M{"id": group.ID}
	update := bson.M{
		"$set": bson.M{
			"name":  group.Name,
			"index": group.Index,
			"utime": group.Utime,
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

// DeleteGroup 删除分组
func (d *modelFieldGroupDAO) DeleteGroup(ctx context.Context, id int64) error {
	col := d.db.Collection(FieldGroupCollection)

	filter := bson.M{"id": id}
	result, err := col.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}

	if result.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}

	return nil
}

// DeleteGroupsByModelUID 删除模型的所有分组
func (d *modelFieldGroupDAO) DeleteGroupsByModelUID(ctx context.Context, modelUID string) error {
	col := d.db.Collection(FieldGroupCollection)

	filter := bson.M{"model_uid": modelUID}
	_, err := col.DeleteMany(ctx, filter)
	return err
}

package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
)

const CollectionModelGroup = "c_model_group"

type ModelGroupDAO struct {
	db *mongox.Mongo
}

func NewModelGroupDAO(db *mongox.Mongo) *ModelGroupDAO {
	return &ModelGroupDAO{db: db}
}

func (d *ModelGroupDAO) Create(ctx context.Context, group domain.ModelGroup) (int64, error) {
	collection := d.db.Collection(CollectionModelGroup)

	// 如果没有设置 ID，则自动生成
	if group.ID == 0 {
		group.ID = d.db.GetIdGenerator(CollectionModelGroup)
	}

	now := time.Now().UnixMilli()
	if group.Ctime == 0 {
		group.Ctime = now
	}
	if group.Utime == 0 {
		group.Utime = now
	}

	_, err := collection.InsertOne(ctx, group)
	if err != nil {
		return 0, err
	}

	return group.ID, nil
}

func (d *ModelGroupDAO) GetByID(ctx context.Context, id int64) (*domain.ModelGroup, error) {
	collection := d.db.Collection(CollectionModelGroup)

	var group domain.ModelGroup
	err := collection.FindOne(ctx, bson.M{"id": id}).Decode(&group)
	if err != nil {
		return nil, err
	}

	return &group, nil
}

func (d *ModelGroupDAO) List(ctx context.Context) ([]domain.ModelGroup, error) {
	collection := d.db.Collection(CollectionModelGroup)

	cursor, err := collection.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var groups []domain.ModelGroup
	if err = cursor.All(ctx, &groups); err != nil {
		return nil, err
	}

	return groups, nil
}

func (d *ModelGroupDAO) Exists(ctx context.Context, id int64) (bool, error) {
	collection := d.db.Collection(CollectionModelGroup)

	count, err := collection.CountDocuments(ctx, bson.M{"id": id})
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

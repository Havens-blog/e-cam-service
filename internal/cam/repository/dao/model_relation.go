package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
)

const CollectionModelRelation = "c_relation_model"

type ModelRelationDAO struct {
	db *mongox.Mongo
}

func NewModelRelationDAO(db *mongox.Mongo) *ModelRelationDAO {
	return &ModelRelationDAO{db: db}
}

func (d *ModelRelationDAO) Create(ctx context.Context, relation domain.ModelRelation) (int64, error) {
	collection := d.db.Collection(CollectionModelRelation)

	relation.ID = d.db.GetIdGenerator(CollectionModelRelation)
	now := time.Now().UnixMilli()
	relation.Ctime = now
	relation.Utime = now

	_, err := collection.InsertOne(ctx, relation)
	if err != nil {
		return 0, err
	}

	return relation.ID, nil
}

func (d *ModelRelationDAO) GetByID(ctx context.Context, id int64) (*domain.ModelRelation, error) {
	collection := d.db.Collection(CollectionModelRelation)

	var relation domain.ModelRelation
	err := collection.FindOne(ctx, bson.M{"id": id}).Decode(&relation)
	if err != nil {
		return nil, err
	}

	return &relation, nil
}

func (d *ModelRelationDAO) List(ctx context.Context) ([]domain.ModelRelation, error) {
	collection := d.db.Collection(CollectionModelRelation)

	cursor, err := collection.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var relations []domain.ModelRelation
	if err = cursor.All(ctx, &relations); err != nil {
		return nil, err
	}

	return relations, nil
}

func (d *ModelRelationDAO) GetBySourceModel(ctx context.Context, sourceModelUID string) ([]domain.ModelRelation, error) {
	collection := d.db.Collection(CollectionModelRelation)

	cursor, err := collection.Find(ctx, bson.M{"source_model_uid": sourceModelUID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var relations []domain.ModelRelation
	if err = cursor.All(ctx, &relations); err != nil {
		return nil, err
	}

	return relations, nil
}

func (d *ModelRelationDAO) Exists(ctx context.Context, sourceModelUID, targetModelUID, relationTypeUID string) (bool, error) {
	collection := d.db.Collection(CollectionModelRelation)

	count, err := collection.CountDocuments(ctx, bson.M{
		"source_model_uid":  sourceModelUID,
		"target_model_uid":  targetModelUID,
		"relation_type_uid": relationTypeUID,
	})
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

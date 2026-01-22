package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
)

const CollectionRelationType = "c_relation_type"

type RelationTypeDAO struct {
	db *mongox.Mongo
}

func NewRelationTypeDAO(db *mongox.Mongo) *RelationTypeDAO {
	return &RelationTypeDAO{db: db}
}

func (d *RelationTypeDAO) Create(ctx context.Context, relationType domain.RelationType) (int64, error) {
	collection := d.db.Collection(CollectionRelationType)

	// 如果没有设置 ID，则自动生成
	if relationType.ID == 0 {
		relationType.ID = d.db.GetIdGenerator(CollectionRelationType)
	}

	now := time.Now().UnixMilli()
	if relationType.Ctime == 0 {
		relationType.Ctime = now
	}
	if relationType.Utime == 0 {
		relationType.Utime = now
	}

	_, err := collection.InsertOne(ctx, relationType)
	if err != nil {
		return 0, err
	}

	return relationType.ID, nil
}

func (d *RelationTypeDAO) GetByUID(ctx context.Context, uid string) (*domain.RelationType, error) {
	collection := d.db.Collection(CollectionRelationType)

	var relationType domain.RelationType
	err := collection.FindOne(ctx, bson.M{"uid": uid}).Decode(&relationType)
	if err != nil {
		return nil, err
	}

	return &relationType, nil
}

func (d *RelationTypeDAO) List(ctx context.Context) ([]domain.RelationType, error) {
	collection := d.db.Collection(CollectionRelationType)

	cursor, err := collection.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var relationTypes []domain.RelationType
	if err = cursor.All(ctx, &relationTypes); err != nil {
		return nil, err
	}

	return relationTypes, nil
}

func (d *RelationTypeDAO) Exists(ctx context.Context, uid string) (bool, error) {
	collection := d.db.Collection(CollectionRelationType)

	count, err := collection.CountDocuments(ctx, bson.M{"uid": uid})
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

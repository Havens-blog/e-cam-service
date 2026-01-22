package dao

import (
	"context"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func InitIndexes(db *mongox.Mongo) error {
	col := db.Collection(EndpointCollection)

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "path", Value: "text"}},
			Options: options.Index().SetDefaultLanguage("english"),
		},
	}

	_, err := col.Indexes().CreateMany(context.Background(), indexes)

	return err
}

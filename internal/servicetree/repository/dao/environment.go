package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const EnvironmentCollection = "c_environment"

// Environment 环境 DAO 模型
type Environment struct {
	ID          int64  `bson:"id"`
	Code        string `bson:"code"`
	Name        string `bson:"name"`
	TenantID    string `bson:"tenant_id"`
	Description string `bson:"description"`
	Color       string `bson:"color"`
	Order       int    `bson:"order"`
	Status      int    `bson:"status"`
	Ctime       int64  `bson:"ctime"`
	Utime       int64  `bson:"utime"`
}

// EnvironmentFilter DAO 层过滤条件
type EnvironmentFilter struct {
	TenantID string
	Code     string
	Status   *int
	Offset   int64
	Limit    int64
}

// EnvironmentDAO 环境数据访问接口
type EnvironmentDAO interface {
	Create(ctx context.Context, env Environment) (int64, error)
	CreateBatch(ctx context.Context, envs []Environment) (int64, error)
	Update(ctx context.Context, env Environment) error
	GetByID(ctx context.Context, id int64) (Environment, error)
	GetByCode(ctx context.Context, tenantID, code string) (Environment, error)
	List(ctx context.Context, filter EnvironmentFilter) ([]Environment, error)
	Count(ctx context.Context, filter EnvironmentFilter) (int64, error)
	Delete(ctx context.Context, id int64) error
}

type environmentDAO struct {
	db *mongox.Mongo
}

// NewEnvironmentDAO 创建环境 DAO
func NewEnvironmentDAO(db *mongox.Mongo) EnvironmentDAO {
	return &environmentDAO{db: db}
}

func (d *environmentDAO) Create(ctx context.Context, env Environment) (int64, error) {
	now := time.Now().UnixMilli()
	env.Ctime = now
	env.Utime = now

	if env.ID == 0 {
		env.ID = d.db.GetIdGenerator(EnvironmentCollection)
	}

	_, err := d.db.Collection(EnvironmentCollection).InsertOne(ctx, env)
	if err != nil {
		return 0, err
	}
	return env.ID, nil
}

func (d *environmentDAO) CreateBatch(ctx context.Context, envs []Environment) (int64, error) {
	if len(envs) == 0 {
		return 0, nil
	}

	now := time.Now().UnixMilli()
	docs := make([]any, len(envs))

	for i := range envs {
		if envs[i].ID == 0 {
			envs[i].ID = d.db.GetIdGenerator(EnvironmentCollection)
		}
		envs[i].Ctime = now
		envs[i].Utime = now
		docs[i] = envs[i]
	}

	result, err := d.db.Collection(EnvironmentCollection).InsertMany(ctx, docs)
	if err != nil {
		return 0, err
	}
	return int64(len(result.InsertedIDs)), nil
}

func (d *environmentDAO) Update(ctx context.Context, env Environment) error {
	env.Utime = time.Now().UnixMilli()

	filter := bson.M{"id": env.ID}
	update := bson.M{
		"$set": bson.M{
			"code":        env.Code,
			"name":        env.Name,
			"description": env.Description,
			"color":       env.Color,
			"order":       env.Order,
			"status":      env.Status,
			"utime":       env.Utime,
		},
	}

	result, err := d.db.Collection(EnvironmentCollection).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (d *environmentDAO) GetByID(ctx context.Context, id int64) (Environment, error) {
	var env Environment
	filter := bson.M{"id": id}
	err := d.db.Collection(EnvironmentCollection).FindOne(ctx, filter).Decode(&env)
	return env, err
}

func (d *environmentDAO) GetByCode(ctx context.Context, tenantID, code string) (Environment, error) {
	var env Environment
	filter := bson.M{"tenant_id": tenantID, "code": code}
	err := d.db.Collection(EnvironmentCollection).FindOne(ctx, filter).Decode(&env)
	return env, err
}

func (d *environmentDAO) List(ctx context.Context, filter EnvironmentFilter) ([]Environment, error) {
	query := d.buildQuery(filter)

	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "order", Value: 1}})

	cursor, err := d.db.Collection(EnvironmentCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var envs []Environment
	err = cursor.All(ctx, &envs)
	return envs, err
}

func (d *environmentDAO) Count(ctx context.Context, filter EnvironmentFilter) (int64, error) {
	query := d.buildQuery(filter)
	return d.db.Collection(EnvironmentCollection).CountDocuments(ctx, query)
}

func (d *environmentDAO) Delete(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := d.db.Collection(EnvironmentCollection).DeleteOne(ctx, filter)
	return err
}

func (d *environmentDAO) buildQuery(filter EnvironmentFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.Code != "" {
		query["code"] = filter.Code
	}
	if filter.Status != nil {
		query["status"] = *filter.Status
	}
	return query
}

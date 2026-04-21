package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const TopoDeclarationsCollection = "topo_declarations"

// DeclarationDAO 声明式注册数据 MongoDB 数据访问对象
type DeclarationDAO struct {
	db *mongox.Mongo
}

// NewDeclarationDAO 创建声明 DAO
func NewDeclarationDAO(db *mongox.Mongo) *DeclarationDAO {
	return &DeclarationDAO{db: db}
}

func (d *DeclarationDAO) col() *mongo.Collection {
	return d.db.Collection(TopoDeclarationsCollection)
}

// Upsert 插入或更新声明数据（按 source + node.id 去重）
func (d *DeclarationDAO) Upsert(ctx context.Context, decl domain.LinkDeclaration) error {
	now := time.Now()
	decl.UpdatedAt = now
	if decl.CreatedAt.IsZero() {
		decl.CreatedAt = now
	}
	if decl.ID == "" {
		decl.ID = decl.Source + ":" + decl.Node.ID
	}
	opts := options.Update().SetUpsert(true)
	_, err := d.col().UpdateOne(ctx, bson.M{"_id": decl.ID}, bson.M{"$set": decl}, opts)
	return err
}

// FindBySource 按上报方标识查询声明数据
func (d *DeclarationDAO) FindBySource(ctx context.Context, tenantID, source string) ([]domain.LinkDeclaration, error) {
	cursor, err := d.col().Find(ctx, bson.M{"tenant_id": tenantID, "source": source})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var decls []domain.LinkDeclaration
	if err = cursor.All(ctx, &decls); err != nil {
		return nil, err
	}
	return decls, nil
}

// FindAll 查询租户下所有声明数据
func (d *DeclarationDAO) FindAll(ctx context.Context, tenantID string) ([]domain.LinkDeclaration, error) {
	cursor, err := d.col().Find(ctx, bson.M{"tenant_id": tenantID},
		options.Find().SetSort(bson.D{{Key: "updated_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var decls []domain.LinkDeclaration
	if err = cursor.All(ctx, &decls); err != nil {
		return nil, err
	}
	return decls, nil
}

// DeleteBySource 按上报方标识批量删除声明数据
func (d *DeclarationDAO) DeleteBySource(ctx context.Context, tenantID, source string) (int64, error) {
	result, err := d.col().DeleteMany(ctx, bson.M{"tenant_id": tenantID, "source": source})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// InitIndexes 初始化声明集合索引
func (d *DeclarationDAO) InitIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "source", Value: 1}}},
	}
	_, err := d.col().Indexes().CreateMany(ctx, indexes)
	return err
}

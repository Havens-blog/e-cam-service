//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	// 初始化日志
	logger := elog.DefaultLogger

	// MongoDB 配置
	mongoURI := "mongodb://ecmdb:123456@118.145.73.93:27017/ecmdb?authSource=admin"
	mongoDatabase := "ecmdb"

	fmt.Printf("🔌 连接到 MongoDB 数据库: %s\n", mongoDatabase)

	// 创建 MongoDB 客户端
	clientOptions := options.Client().ApplyURI(mongoURI)
	client, err := mongo.Connect(context.Background(), clientOptions)
	if err != nil {
		fmt.Printf("❌ 连接数据库失败: %v\n", err)
		os.Exit(1)
	}
	defer client.Disconnect(context.Background())

	// 测试连接
	if err := client.Ping(context.Background(), nil); err != nil {
		fmt.Printf("❌ Ping 数据库失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ 数据库连接成功")

	// 初始化数据库连接
	db := mongox.NewMongo(client, mongoDatabase)

	// 初始化索引
	fmt.Println("📊 初始化数据库索引...")
	if err := dao.InitIndexes(db); err != nil {
		fmt.Printf("⚠️  索引初始化警告: %v (可能索引已存在，继续执行)\n", err)
	} else {
		fmt.Println("✅ 索引初始化完成")
	}

	// 创建 DAO
	modelDAO := dao.NewModelDAO(db)
	fieldDAO := dao.NewModelFieldDAO(db)
	groupDAO := dao.NewModelFieldGroupDAO(db)
	modelGroupDAO := dao.NewModelGroupDAO(db)
	relationTypeDAO := dao.NewRelationTypeDAO(db)
	modelRelationDAO := dao.NewModelRelationDAO(db)

	// 创建 Repository
	modelRepo := repository.NewModelRepository(modelDAO)
	fieldRepo := repository.NewModelFieldRepository(fieldDAO)
	groupRepo := repository.NewModelFieldGroupRepository(groupDAO)

	// 创建初始化器
	initializer := service.NewModelInitializer(
		modelRepo,
		fieldRepo,
		groupRepo,
		modelGroupDAO,
		relationTypeDAO,
		modelRelationDAO,
		logger,
	)

	// 执行初始化
	ctx := context.Background()
	fmt.Println("🚀 开始初始化云资源模型...")

	if err := initializer.InitializeModels(ctx); err != nil {
		fmt.Printf("❌ 初始化失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ 云资源模型初始化完成！")
}

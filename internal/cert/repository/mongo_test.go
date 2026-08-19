package repository

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// mongox test 实例启动方式：
// 优先读取环境变量 CERT_TEST_MONGODB_DSN（例：mongodb://admin:password@127.0.0.1:27017），
// 缺省回退本地 docker-compose MongoDB（deploy/docker-compose.yml 暴露 27017）。
// 连接与探活在进程内只做一次；实例不可达时跳过集成测试
// （保持无 DB 环境下 go test 通过）。每个用例使用独立的随机数据库并在
// 结束后整体 Drop，不污染共享数据。
var (
	testMongoOnce   sync.Once
	testMongoClient *mongo.Client
	testMongoErr    error
)

func connectTestMongo() {
	dsn := "mongodb://127.0.0.1:27017"
	if v := os.Getenv("CERT_TEST_MONGODB_DSN"); v != "" {
		dsn = v
	}
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(dsn))
	if err != nil {
		testMongoClient, testMongoErr = nil, err
		return
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, readpref.PrimaryPreferred()); err != nil {
		_ = client.Disconnect(context.Background())
		testMongoClient, testMongoErr = nil, err
		return
	}
	testMongoClient, testMongoErr = client, nil
}

// newTestMongo 返回独立随机数据库的 mongox.Mongo（用例隔离；结束后 Drop）。
// 无可用 MongoDB 时跳过当前用例。
func newTestMongo(t *testing.T) *mongox.Mongo {
	t.Helper()
	testMongoOnce.Do(connectTestMongo)
	if testMongoErr != nil {
		t.Skipf("mongox test 实例不可用（可设置 CERT_TEST_MONGODB_DSN）: %v", testMongoErr)
	}
	dbName := fmt.Sprintf("ecam_cert_test_%d", rand.Int63())
	db := mongox.NewMongo(testMongoClient, dbName)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = db.Database().Drop(ctx)
	})
	return db
}

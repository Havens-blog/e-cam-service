package cert

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	accountdao "github.com/Havens-blog/e-cam-service/internal/account/repository/dao"
	assetrepo "github.com/Havens-blog/e-cam-service/internal/asset/repository"
	assetdao "github.com/Havens-blog/e-cam-service/internal/asset/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// mongox test 实例启动方式与 repository/mongo_test.go 同约定：
// CERT_TEST_MONGODB_DSN（缺省回退本地 27017），不可达即 skip。
var (
	smokeMongoOnce   sync.Once
	smokeMongoClient *mongo.Client
	smokeMongoErr    error
)

func connectSmokeMongo() {
	dsn := "mongodb://127.0.0.1:27017"
	if v := os.Getenv("CERT_TEST_MONGODB_DSN"); v != "" {
		dsn = v
	}
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(dsn))
	if err != nil {
		smokeMongoClient, smokeMongoErr = nil, err
		return
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, readpref.PrimaryPreferred()); err != nil {
		_ = client.Disconnect(context.Background())
		smokeMongoClient, smokeMongoErr = nil, err
		return
	}
	smokeMongoClient, smokeMongoErr = client, nil
}

func newSmokeMongo(t *testing.T) *mongox.Mongo {
	t.Helper()
	smokeMongoOnce.Do(connectSmokeMongo)
	if smokeMongoErr != nil {
		t.Skipf("mongox test 实例不可用（可设置 CERT_TEST_MONGODB_DSN）: %v", smokeMongoErr)
	}
	dbName := fmt.Sprintf("ecam_cert_smoke_%d", rand.Int63())
	db := mongox.NewMongo(smokeMongoClient, dbName)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = db.Database().Drop(ctx)
	})
	return db
}

// TestInitCertModule_BootSmoke AC-5 启动冒烟（mongox test 实例）：
// 全量装配 boot 无错 + 手动触发 scan/inspection 各一次无 panic + 路由挂载。
// 主密钥经 t.Setenv 注入（1.1 fail-fast 契约：真实缺密钥场景在 ioc 装配
// 启动失败，此处验证"配置齐备时全链路可启动"路径）。
func TestInitCertModule_BootSmoke(t *testing.T) {
	db := newSmokeMongo(t)
	t.Setenv("EIAM_CERT_MASTER_KEY_V1", smokeMasterKeyB64)

	queue := taskx.NewQueue(taskx.NewMongoRepository(db, "ecam_task"), elog.DefaultLogger, taskx.Config{WorkerNum: 1, BufferSize: 8})
	queue.Start()
	t.Cleanup(queue.Stop)

	module, err := InitCertModule(
		db,
		elog.DefaultLogger,
		accountrepo.NewCloudAccountRepository(accountdao.NewCloudAccountDAO(db)),
		assetrepo.NewInstanceRepository(assetdao.NewInstanceDAO(db)),
		queue,
		service.NewLoggingAlertPublisher(),
	)
	if err != nil {
		t.Fatalf("cert 模块装配失败: %v", err)
	}
	// 装配完备性：调度面 + Web 面全量非 nil（AC-2 注入面）。
	if module.ScanSvc == nil || module.InspectionJob == nil || module.VerifyWindowSvc == nil ||
		module.ChangeSvc == nil || module.ExecuteSvc == nil || module.OrphanCleanupSvc == nil ||
		module.CrdRecheckSvc == nil || module.K8sCredSvc == nil || module.AlertPublisher == nil {
		t.Fatal("调度面服务存在 nil 字段")
	}
	if module.Hdl == nil || module.ReferenceHdl == nil || module.LedgerHdl == nil ||
		module.DashboardHdl == nil || module.SettingsHdl == nil || module.ChangeHdl == nil {
		t.Fatal("Web 面 handler 存在 nil 字段")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 手动触发 scan 一次：无 active 账号+无登记 → 空范围显式失败（非 panic）。
	result, err := module.ScanSvc.StartScan(ctx)
	if err != nil {
		t.Fatalf("手动扫描触发失败: %v", err)
	}
	if result.Status != domain.ScanStatusFailed || result.FailReason != domain.FailReasonScanNoChannels {
		t.Fatalf("空范围扫描期望 failed/%s，实际 %s/%s", domain.FailReasonScanNoChannels, result.Status, result.FailReason)
	}
	// 手动触发 inspection 一次：空台账一轮完整流水线（无 panic 即过）。
	if _, err := module.InspectionJob.RunInspection(ctx); err != nil {
		t.Fatalf("手动巡检触发失败: %v", err)
	}

	// 路由挂载：/api/v1/certs 前缀进入路由树。
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	module.RegisterRoutes(engine)
	hasPrefix := false
	for _, r := range engine.Routes() {
		if len(r.Path) >= len("/api/v1/certs") && r.Path[:len("/api/v1/certs")] == "/api/v1/certs" {
			hasPrefix = true
			break
		}
	}
	if !hasPrefix {
		t.Fatal("证书域路由未挂载 /api/v1/certs 前缀")
	}

	// 9 类调度点全量可执行（fake 无业务数据，扫描/巡检为第二轮幂等重入）。
	jobs := &scheduler.CertJobs{
		Scan:       module.ScanSvc,
		Inspection: module.InspectionJob,
		Windows:    module.VerifyWindowSvc,
		Changes:    module.ChangeSvc,
		Execute:    module.ExecuteSvc,
		Orphan:     module.OrphanCleanupSvc,
		Recheck:    module.CrdRecheckSvc,
		Publisher:  module.AlertPublisher,
	}
	for _, spec := range jobs.JobSpecs(10) {
		runCtx, runCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := spec.Run(runCtx); err != nil {
			t.Errorf("调度点 %s 执行失败: %v", spec.Name, err)
		}
		runCancel()
	}
}

// smokeMasterKeyB64 32 字节测试主密钥（base64）——仅测试用，非生产密钥材料。
const smokeMasterKeyB64 = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="

// TestUnavailableDispatcher_QueuelessBootDegradation 队列缺失时装配不阻断：
// 派发器显式报错（boot 可用，批量执行入口运行期报错）。
func TestUnavailableDispatcher_QueuelessBootDegradation(t *testing.T) {
	db := newSmokeMongo(t)
	t.Setenv("EIAM_CERT_MASTER_KEY_V1", smokeMasterKeyB64)

	module, err := InitCertModule(
		db,
		elog.DefaultLogger,
		accountrepo.NewCloudAccountRepository(accountdao.NewCloudAccountDAO(db)),
		assetrepo.NewInstanceRepository(assetdao.NewInstanceDAO(db)),
		nil, // 无任务队列：降级装配
		nil, // 无告警发布器：回退日志发布
	)
	if err != nil {
		t.Fatalf("无队列装配应成功: %v", err)
	}
	if module.ExecuteSvc == nil {
		t.Fatal("执行服务应完成装配")
	}
}

// TestRegisterRoutes_NilSafe 空引擎/空模块安全（防御式装配）。
func TestRegisterRoutes_NilSafe(t *testing.T) {
	(&Module{}).RegisterRoutes(nil) // 不应 panic
	module := &Module{}
	module.RegisterRoutes(gin.New())
}

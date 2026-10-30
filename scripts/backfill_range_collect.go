//go:build ignore

// backfill_range_collect 指定账期范围补采指定云厂商的账单明细。
//
// 背景：采集器曾有两个缺陷——
//  1. 增量起点被历史失败日志永久钉住（collector.calculateIncrementalRange）；
//  2. tencent/volcano/huawei 适配器只拉取 StartTime 所在单月。
//
// 两处均已修复（增量从此正确），但"从未成功采集过的中间账期"增量推算
// 永远覆盖不到，需要本脚本按显式范围一次性补齐（或在「采集管理」页
// 手动触发等价范围，两者走同一条生产采集路径）。
//
// 本脚本完整复用 CollectorService.CollectAccount：账号级分布式锁、采集
// 日志、按月先删后插去重、标准化入库、汇总表自动刷新，仅把范围来源从
// "增量推算"换成命令行显式指定。
//
// 用法（默认干跑，只打印采集计划，不写任何数据）：
//
//	go run scripts/backfill_range_collect.go -provider tencent -start 2026-04-01
//	go run scripts/backfill_range_collect.go -provider volcengine -start 2025-12-01 -end 2026-10-27
//
// 实际执行（-apply）：
//
//	go run scripts/backfill_range_collect.go -provider tencent -start 2026-04-01 -apply
//
// 说明：对云厂商侧为只读账单查询（与 6 小时定时采集同类操作），
// 不操作任何云资源；写入仅发生在本平台数据库。

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	accountdao "github.com/Havens-blog/e-cam-service/internal/account/repository/dao"
	accountservice "github.com/Havens-blog/e-cam-service/internal/account/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/collector"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/normalizer"
	costdao "github.com/Havens-blog/e-cam-service/internal/cam/cost/repository/dao"
	shareddomain "github.com/Havens-blog/e-cloudx-sdk/domain"
	_ "github.com/Havens-blog/e-cloudx-sdk/providers" // 注册各云 BillingAdapter（init）
	"github.com/Havens-blog/e-common-go/crypto"
	"github.com/Havens-blog/e-common-go/mongox"
	"github.com/gotomicro/ego/core/elog"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 连接信息一律来自环境变量（源码不再内置任何生产地址与口令）：
//
//	ECAM_MONGODB_DSN      生产 Mongo DSN（必填）
//	ECAM_MONGODB_NAME     Mongo 库名，默认 ecam
//	ECAM_REDIS_ADDR       Redis 地址（必填）
//	ECAM_REDIS_PASSWORD   Redis 口令（按部署实际情况，可为空）
var (
	mongoURI  = os.Getenv("ECAM_MONGODB_DSN")
	mongoDB   = envOr("ECAM_MONGODB_NAME", "ecam")
	redisAddr = os.Getenv("ECAM_REDIS_ADDR")
	redisPass = os.Getenv("ECAM_REDIS_PASSWORD")
	redisDB   = 1
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	if mongoURI == "" || redisAddr == "" {
		log.Fatal("缺少连接信息：请设置环境变量 ECAM_MONGODB_DSN 与 ECAM_REDIS_ADDR（脚本不再内置生产地址/口令）")
	}
	provider := flag.String("provider", "", "云厂商（tencent/volcengine/huawei/aws/aliyun），必填")
	startStr := flag.String("start", "", "账期范围起始日 YYYY-MM-DD，必填")
	endStr := flag.String("end", "", "账期范围结束日 YYYY-MM-DD，默认今天")
	apply := flag.Bool("apply", false, "实际执行采集（默认干跑）")
	configDir := flag.String("config", "config", "服务配置目录（读取 security.encryption_key）")
	flag.Parse()

	if *provider == "" || *startStr == "" {
		fmt.Println("必须指定 -provider 与 -start；-apply 实际执行，默认干跑")
		flag.Usage()
		os.Exit(2)
	}
	start, err := time.ParseInLocation("2006-01-02", *startStr, time.Local)
	if err != nil {
		log.Fatalf("解析 -start 失败: %v", err)
	}
	end := time.Now()
	if *endStr != "" {
		end, err = time.ParseInLocation("2006-01-02", *endStr, time.Local)
		if err != nil {
			log.Fatalf("解析 -end 失败: %v", err)
		}
		end = end.Add(24*time.Hour - time.Second) // 含结束日当天
	}
	if end.Before(start) {
		log.Fatalf("-end 早于 -start")
	}

	// ---- 初始化云账号密钥解密组件 ----
	// 与 cmd/start/start.go 同源：security.encryption_key（可被 CAM_ENCRYPTION_KEY
	// 环境变量覆盖）。账号仓库对密文解密失败时会静默回退为密文本身，导致云 API
	// 签名错误，因此这里初始化失败必须直接退出，而不是带病继续。
	key := os.Getenv("CAM_ENCRYPTION_KEY")
	if key == "" {
		v := viper.New()
		v.SetConfigFile(filepath.Join(*configDir, "prod.yaml"))
		if err := v.ReadInConfig(); err != nil {
			log.Fatalf("读取配置 %s 失败: %v（或改用环境变量 CAM_ENCRYPTION_KEY）", *configDir, err)
		}
		key = v.GetString("security.encryption_key")
	}
	if err := crypto.InitDefaultCrypto(key); err != nil {
		log.Fatalf("初始化加密组件失败: %v（检查 security.encryption_key / CAM_ENCRYPTION_KEY）", err)
	}

	ctx := context.Background()

	// ---- 连接存储 ----
	mclient, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI).SetServerSelectionTimeout(15*time.Second))
	if err != nil {
		log.Fatalf("连接 MongoDB: %v", err)
	}
	defer mclient.Disconnect(ctx)
	if err := mclient.Ping(ctx, nil); err != nil {
		log.Fatalf("ping MongoDB: %v", err)
	}
	db := mongox.NewMongo(mclient, mongoDB)

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr, Password: redisPass, DB: redisDB})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("ping Redis: %v", err)
	}

	// ---- 按生产装配构建采集服务（与 internal/cam/init.go initCostModule 一致）----
	logger := elog.DefaultLogger
	billDAO := costdao.NewBillDAO(db)
	collectLogDAO := costdao.NewCollectLogDAO(db)
	normalizerSvc := normalizer.NewNormalizerService(billDAO, logger)
	accountRepo := accountrepo.NewCloudAccountRepository(accountdao.NewCloudAccountDAO(db))
	accountSvc := accountservice.NewCloudAccountService(accountRepo, nil, logger)
	collectorSvc := collector.NewCollectorService(normalizerSvc, billDAO, collectLogDAO, accountSvc, rdb, logger)
	collectorSvc.SetSummaryRefresher(costdao.NewDailySummaryDAO(db, logger))

	// ---- 选择账号 ----
	accounts, total, err := accountSvc.ListAccounts(ctx, shareddomain.CloudAccountFilter{
		Status: shareddomain.CloudAccountStatusActive,
		Limit:  100,
	})
	if err != nil {
		log.Fatalf("查询云账号: %v", err)
	}
	fmt.Printf("活跃账号共 %d 个。\n\n", total)

	var targets []shareddomain.CloudAccount
	for _, acct := range accounts {
		if strings.EqualFold(string(acct.Provider), *provider) {
			targets = append(targets, *acct)
		}
	}
	if len(targets) == 0 {
		log.Fatalf("未找到 provider=%s 的活跃账号", *provider)
	}

	months := collectMonths(start, end)
	fmt.Printf("补采计划 provider=%s 范围 %s ~ %s（覆盖账期月: %s）apply=%v\n",
		*provider, start.Format("2006-01-02"), end.Format("2006-01-02"),
		strings.Join(months, ", "), *apply)
	fmt.Printf("目标账号:\n")
	for _, acct := range targets {
		fmt.Printf("  id=%d name=%s provider=%s tenant=%d\n", acct.ID, acct.Name, acct.Provider, acct.TenantID)
	}
	if !*apply {
		fmt.Println("\n干跑结束（未写任何数据）。确认无误后加 -apply 执行。")
		return
	}

	// ---- 逐账号补采（顺序执行，避免并发打爆云 API 配额）----
	for _, acct := range targets {
		fmt.Printf("\n===== 开始采集 account=%d(%s) =====\n", acct.ID, acct.Name)
		collectStart := time.Now()
		if err := collectorSvc.CollectAccount(ctx, acct.ID, start, end); err != nil {
			fmt.Printf("!! 采集失败 account=%d: %v（详情见采集日志与 e-cam-service 日志）\n", acct.ID, err)
			continue
		}
		fmt.Printf("===== 采集完成 account=%d 耗时 %s =====\n", acct.ID, time.Since(collectStart).Round(time.Second))
	}
	fmt.Println("\n全部目标账号处理完毕。汇总表已由采集流程按本次范围自动重建。")
}

// collectMonths 与 collector.collectBillingMonths 相同语义：范围覆盖的全部账期月。
func collectMonths(startTime, endTime time.Time) []string {
	var months []string
	current := time.Date(startTime.Year(), startTime.Month(), 1, 0, 0, 0, 0, time.UTC)
	endMonth := time.Date(endTime.Year(), endTime.Month(), 1, 0, 0, 0, 0, time.UTC)
	for !current.After(endMonth) {
		months = append(months, current.Format("2006-01"))
		current = current.AddDate(0, 1, 0)
	}
	return months
}

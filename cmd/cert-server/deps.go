package main

// cert-server 独立装配（cert 服务抽取 · 可独立部署入口）。
//
// 设计原则：
//  1. 不 import ioc——ioc 牵连全部域（cam/cmdb/alert…），一旦 import 整个单体即被
//     拖进本二进制，违背"可独立部署"。本文件自带精简 builder，仅依赖 cert 域、
//     其出站端口的本地实现、以及 eiam 鉴权库。
//  2. 鉴权 fail-closed——复用单体已验证的 EcmdbAuthMiddleware + redis 会话；
//     配置缺失即 panic 拒启，cert 路由绝不无认证裸露。
//  3. 出站端口（account/asset/audit）暂用同库仓储的本地适配器；cert 抽到独立仓
//     时，这些 adapter 替换为对 account/asset/audit 服务的 HTTP client 即可，
//     cert 域本身零改动（出站已全部端口化）。queue/dns 传 nil（降级：批量派发
//     显式报错、probe 回退台账 SAN），独立服务首版不接 cam 任务队列与 DNS 源。

import (
	"context"
	"fmt"
	"time"

	assetdomain "github.com/Havens-blog/e-cam-service/internal/asset/domain"
	auditdomain "github.com/Havens-blog/e-cam-service/internal/audit/domain"
	auditservice "github.com/Havens-blog/e-cam-service/internal/audit/service"
	"github.com/Havens-blog/e-cam-service/internal/cert"
	certservice "github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-common-go/mongox"
	"github.com/gotomicro/ego/core/elog"
	goRedis "github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// buildMongo 独立 MongoDB 连接（读 viper mongodb.*；必填校验，缺失即 panic）。
func buildMongo() (*mongox.Mongo, error) {
	type config struct {
		DSN      string `mapstructure:"dsn"`
		DB       string `mapstructure:"db"`
		Username string `mapstructure:"username"`
		Password string `mapstructure:"password"`
	}
	var cfg config
	if err := viper.UnmarshalKey("mongodb", &cfg); err != nil {
		return nil, fmt.Errorf("read mongodb config: %w", err)
	}
	if cfg.DSN == "" || cfg.DB == "" {
		return nil, fmt.Errorf("mongodb dsn and db are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	uri := cfg.DSN
	if cfg.Username != "" && cfg.Password != "" {
		// DSN 形如 mongodb://host:port/...，注入凭据（与单体 ioc/db.go 同约定）。
		const sep = "//"
		if i := indexOf(cfg.DSN, sep); i >= 0 {
			uri = cfg.DSN[:i+len(sep)] + cfg.Username + ":" + cfg.Password + "@" + cfg.DSN[i+len(sep):]
		}
	}
	opts := options.Client().
		ApplyURI(uri).
		SetCompressors([]string{"zstd", "snappy"}).
		SetMaxPoolSize(100).
		SetServerSelectionTimeout(5 * time.Second).
		SetConnectTimeout(10 * time.Second)

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("connect mongodb: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("ping mongodb: %w", err)
	}
	return mongox.NewMongo(client, cfg.DB), nil
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// buildRedis 独立 redis 客户端（读 viper redis.*；addr 必填，缺失即 panic）。
func buildRedis() (*goRedis.Client, error) {
	type config struct {
		Addr     string `mapstructure:"addr"`
		Password string `mapstructure:"password"`
		DB       int    `mapstructure:"db"`
	}
	var cfg config
	if err := viper.UnmarshalKey("redis", &cfg); err != nil {
		return nil, fmt.Errorf("read redis config: %w", err)
	}
	if cfg.Addr == "" {
		return nil, fmt.Errorf("redis addr is required")
	}
	return goRedis.NewClient(&goRedis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	}), nil
}

// ---- 出站端口的本地适配器（抽到独立仓时替换为 HTTP client） ----

// localInstanceCounter 把 cert InstanceCounter 适配到 asset 仓储 Count。
type localInstanceCounter struct {
	repo interface {
		Count(ctx context.Context, filter assetdomain.InstanceFilter) (int64, error)
	}
}

func (a localInstanceCounter) CountByModelUID(ctx context.Context, modelUID string) (int64, error) {
	return a.repo.Count(ctx, assetdomain.InstanceFilter{ModelUID: modelUID})
}

// localAuditStore 把 cert ChangeAuditStore 适配到 internal/audit 审计服务。
type localAuditStore struct {
	svc *auditservice.ChangeOrderAuditService
}

func (a localAuditStore) Record(ctx context.Context, e certservice.ChangeAuditEntry) error {
	return a.svc.Record(ctx, toAuditEntry(e))
}

func (a localAuditStore) RecordDedup(ctx context.Context, e certservice.ChangeAuditEntry) (bool, error) {
	return a.svc.RecordDedup(ctx, toAuditEntry(e))
}

func (a localAuditStore) ListByOrder(ctx context.Context, orderID string) ([]certservice.ChangeAuditEntry, error) {
	entries, err := a.svc.ListByOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	return fromAuditEntries(entries), nil
}

func (a localAuditStore) ListOrphanCleanupResults(ctx context.Context, orderID string) ([]certservice.ChangeAuditEntry, error) {
	entries, err := a.svc.ListOrphanCleanupResults(ctx, orderID)
	if err != nil {
		return nil, err
	}
	return fromAuditEntries(entries), nil
}

func (a localAuditStore) ListUnmetDomains(ctx context.Context, orderID string) ([]string, error) {
	return a.svc.ListUnmetDomains(ctx, orderID)
}

func toAuditEntry(e certservice.ChangeAuditEntry) auditdomain.ChangeOrderAuditEntry {
	return auditdomain.ChangeOrderAuditEntry{
		OrderID: e.OrderID, ItemID: e.ItemID, Actor: e.Actor, Action: e.Action,
		Detail: e.Detail, At: e.At, Cloud: e.Cloud, CloudCertID: e.CloudCertID,
		OrphanAction: e.OrphanAction, Success: e.Success, UnmetDomains: e.UnmetDomains,
		DedupKey: e.DedupKey,
	}
}

func fromAuditEntries(src []auditdomain.ChangeOrderAuditEntry) []certservice.ChangeAuditEntry {
	out := make([]certservice.ChangeAuditEntry, len(src))
	for i, e := range src {
		out[i] = certservice.ChangeAuditEntry{
			OrderID: e.OrderID, ItemID: e.ItemID, Actor: e.Actor, Action: e.Action,
			Detail: e.Detail, At: e.At, Cloud: e.Cloud, CloudCertID: e.CloudCertID,
			OrphanAction: e.OrphanAction, Success: e.Success, UnmetDomains: e.UnmetDomains,
			DedupKey: e.DedupKey,
		}
	}
	return out
}

// buildCertModule 装配 cert 域（独立服务版）：account/asset/audit 走本地适配器，
// publisher 走 cert 自带日志降级，queue/dns 传 nil（降级，见文件头说明）。
func buildCertModule(db *mongox.Mongo, logger *elog.Component) (*cert.Module, error) {
	// cloud-account-lister 支持 local/remote 形态切换（cert-server.upstream.cloud_account_lister；
	// 默认 local）。remote 携带 AK/SK，selector 施加 https 安全闸，见 remote.go。
	accounts, err := selectCloudAccountLister(db)
	if err != nil {
		return nil, err
	}

	// instance-counter 支持 local/remote 形态切换（cert-server.upstream.instance_counter；
	// 默认 local）。远程形态走 HTTP 内部契约，见 remote.go。
	instances, err := selectInstanceCounter(db)
	if err != nil {
		return nil, err
	}

	// audit-store 支持 local/remote 形态切换（cert-server.upstream.audit_store；
	// 默认 local）。远程形态走 HTTP 内部契约（含写路径 + dedup 幂等），见 remote.go。
	audits, err := selectAuditStore(db, logger)
	if err != nil {
		return nil, err
	}

	return cert.InitCertModule(
		db,
		logger,
		accounts,
		instances,
		nil,                                    // queue：独立服务首版不接 cam 任务队列（批量派发降级显式报错）
		certservice.NewLoggingAlertPublisher(), // 告警降级为日志（独立服务不接 alert 基建）
		nil,                                    // dnsSource：不接 cam DNS 源（probe 回退台账 SAN）
		audits,
	)
}

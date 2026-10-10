// Command cert-server 独立运行 cert 证书管理域（cert 服务抽取 · 可独立部署入口）。
//
// 本二进制不 import ioc，仅装配 cert 域 + 其出站端口的本地实现 + eiam 鉴权库，
// 为 cert 抽到独立仓做准备（见 deps.go 头部说明）。鉴权 fail-closed：会话/鉴权
// 配置缺失即拒绝启动，cert 路由绝不无认证裸露。
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-common-go/crypto"
	"github.com/ecodeclub/ginx/session"
	"github.com/ecodeclub/ginx/session/cookie"
	"github.com/ecodeclub/ginx/session/header"
	"github.com/ecodeclub/ginx/session/mixin"
	ginRedis "github.com/ecodeclub/ginx/session/redis"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	goRedis "github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

func main() {
	configFile := "config/prod.yaml"
	if len(os.Args) > 1 {
		configFile = os.Args[1]
	}
	viper.SetConfigFile(configFile)
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "cert-server: 读取配置文件失败: %v\n", err)
		os.Exit(1)
	}

	logger := elog.DefaultLogger

	// 加密器（cert 信封加密的 fallback 主密钥源：security.encryption_key）。
	// 缺失不致命——cert 装配期若主密钥完全不可用会 fail-fast 报错。
	if err := crypto.InitDefaultCrypto(viper.GetString("security.encryption_key")); err != nil {
		logger.Warn("cert-server: 加密组件初始化告警", elog.FieldErr(err))
	}

	if err := run(logger); err != nil {
		logger.Error("cert-server: 启动失败", elog.FieldErr(err))
		os.Exit(1)
	}
}

func run(logger *elog.Component) error {
	// ---- 持久化 + 会话存储 ----
	db, err := buildMongo()
	if err != nil {
		return fmt.Errorf("build mongo: %w", err)
	}
	redisCli, err := buildRedis()
	if err != nil {
		return fmt.Errorf("build redis: %w", err)
	}

	// ---- 会话提供者（fail-closed：鉴权配置缺失即拒启） ----
	sp, err := buildSessionProvider(redisCli)
	if err != nil {
		return fmt.Errorf("build session provider: %w", err)
	}
	session.SetDefaultProvider(sp)

	// ---- cert 域装配 ----
	certModule, err := buildCertModule(db, logger)
	if err != nil {
		return fmt.Errorf("build cert module: %w", err)
	}

	// ---- HTTP 服务器 ----
	gin.SetMode(gin.ReleaseMode)
	server := gin.Default()
	server.ContextWithFallback = true

	// 健康检查（白名单，无需认证）。
	server.GET("/api/v1/certs/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"code": 0, "msg": "ok"})
	})

	// 认证中间件（复用单体已验证实现）：健康检查走白名单，其余一律校验会话。
	// fail-closed：未认证请求在此 401，不会到达 cert 路由。
	var authCfg middleware.AuthConfig
	_ = viper.UnmarshalKey("auth", &authCfg)
	ensureHealthWhitelisted(&authCfg)
	server.Use(middleware.EcmdbAuthMiddlewareWithConfig(sp, authCfg, logger))

	// cert 域路由（/api/v1/certs，端点级 RequireRoles 角色门卫在 handler 内）。
	certModule.RegisterRoutes(server)

	type svcConfig struct {
		Port string `mapstructure:"port"`
	}
	var cfg svcConfig
	if err := viper.UnmarshalKey("cert-server", &cfg); err != nil {
		return fmt.Errorf("read cert-server config: %w", err)
	}
	if cfg.Port == "" {
		cfg.Port = "8081"
		logger.Info("cert-server: 未配置 cert-server.port，使用默认端口 8081")
	}

	logger.Info("cert-server: 启动 HTTP 服务", elog.String("port", cfg.Port))
	return server.Run(":" + cfg.Port)
}

// buildSessionProvider 复刻单体 ioc.InitSessionProvider：redis 会话 + cookie/header
// 双载体。fail-closed：session_encrypted_key / cookie 配置缺失即报错拒启。
func buildSessionProvider(cmd goRedis.Cmdable) (session.Provider, error) {
	type config struct {
		SessionEncryptedKey string `mapstructure:"session_encrypted_key"`
		Cookie              struct {
			Domain string `mapstructure:"domain"`
			Name   string `mapstructure:"name"`
		} `mapstructure:"cookie"`
	}
	var cfg config
	if err := viper.UnmarshalKey("session", &cfg); err != nil {
		return nil, fmt.Errorf("read session config: %w", err)
	}
	if cfg.SessionEncryptedKey == "" {
		return nil, fmt.Errorf("session.session_encrypted_key is required (fail-closed: refuse to serve cert routes without session auth)")
	}
	if cfg.Cookie.Name == "" || cfg.Cookie.Domain == "" {
		return nil, fmt.Errorf("session.cookie.name and session.cookie.domain are required")
	}

	const day = time.Hour * 24 * 30
	sp := ginRedis.NewSessionProvider(cmd, cfg.SessionEncryptedKey, day)
	cookieC := &cookie.TokenCarrier{
		MaxAge:   int(day.Seconds()),
		Name:     cfg.Cookie.Name,
		Secure:   true,
		HttpOnly: false,
		Domain:   cfg.Cookie.Domain,
	}
	sp.TokenCarrier = mixin.NewTokenCarrier(header.NewTokenCarrier(), cookieC)
	return sp, nil
}

// ensureHealthWhitelisted 保证健康检查路径在白名单内（无需认证）。
func ensureHealthWhitelisted(cfg *middleware.AuthConfig) {
	const health = "/api/v1/certs/health"
	for _, w := range cfg.Whitelist {
		if w == health {
			return
		}
	}
	cfg.Whitelist = append(cfg.Whitelist, health)
}

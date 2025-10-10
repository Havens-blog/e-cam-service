// Copyright 2023 ecodeclub
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ioc

import (
	"fmt"
	"time"

	"github.com/ecodeclub/ginx/session"
	"github.com/ecodeclub/ginx/session/cookie"
	"github.com/ecodeclub/ginx/session/header"
	"github.com/ecodeclub/ginx/session/mixin"
	ginRedis "github.com/ecodeclub/ginx/session/redis"
	"github.com/gin-contrib/sessions"
	cookieSession "github.com/gin-contrib/sessions/cookie"
	redisStore "github.com/gin-contrib/sessions/redis"
	"github.com/gotomicro/ego/core/elog"
	goRedis "github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

func InitSessionStore(cmd goRedis.Cmdable) sessions.Store {
	logger := elog.DefaultLogger
	logger.Info("开始初始化会话存储")

	// 尝试创建Redis session store
	client := cmd.(*goRedis.Client)
	addr := client.Options().Addr
	password := client.Options().Password

	logger.Info("尝试创建Redis会话存储", elog.String("addr", addr))
	store, err := redisStore.NewStore(10, "tcp", addr, "", password, []byte("secret"))

	if err != nil {
		// 如果Redis连接失败，使用cookie store作为备选
		logger.Warn("Redis会话存储创建失败，降级使用Cookie存储", elog.FieldErr(err))
		cookieStore := cookieSession.NewStore([]byte("secret"))
		logger.Info("Cookie会话存储初始化完成")
		return cookieStore
	}

	logger.Info("Redis会话存储初始化完成")
	return store
}

func InitSessionProvider(cmd goRedis.Cmdable) session.Provider {
	logger := elog.DefaultLogger
	logger.Info("开始初始化会话提供者")

	type Config struct {
		SessionEncryptedKey string `mapstructure:"session_encrypted_key"`
		Cookie              struct {
			Domain string `mapstructure:"domain"`
			Name   string `mapstructure:"name"`
		} `mapstructure:"cookie"`
	}
	var cfg Config

	err := viper.UnmarshalKey("session", &cfg)
	if err != nil {
		logger.Error("读取会话配置失败", elog.FieldErr(err))
		panic(fmt.Errorf("failed to read session config: %w", err))
	}

	// 验证必需的配置项
	if cfg.SessionEncryptedKey == "" {
		logger.Error("会话加密密钥配置缺失")
		panic(fmt.Errorf("session_encrypted_key is required but not configured"))
	}
	if cfg.Cookie.Name == "" {
		logger.Error("Cookie名称配置缺失")
		panic(fmt.Errorf("cookie.name is required but not configured"))
	}
	if cfg.Cookie.Domain == "" {
		logger.Error("Cookie域名配置缺失")
		panic(fmt.Errorf("cookie.domain is required but not configured"))
	}

	logger.Info("会话配置验证通过",
		elog.String("cookie_name", cfg.Cookie.Name),
		elog.String("cookie_domain", cfg.Cookie.Domain))

	const day = time.Hour * 24 * 30
	sp := ginRedis.NewSessionProvider(cmd, cfg.SessionEncryptedKey, day)
	cookieC := &cookie.TokenCarrier{
		MaxAge:   int(day.Seconds()),
		Name:     cfg.Cookie.Name,
		Secure:   true,
		HttpOnly: false,
		Domain:   cfg.Cookie.Domain,
	}
	headerC := header.NewTokenCarrier()
	sp.TokenCarrier = mixin.NewTokenCarrier(headerC, cookieC)

	logger.Info("会话提供者初始化完成")
	return sp
}

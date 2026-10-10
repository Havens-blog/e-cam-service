package main

// cert-server 鉴权装配的 fail-closed 回归测试。
//
// 守护一个安全敏感不变量：会话/cookie 配置缺失时，buildSessionProvider 必须
// 返回错误（上游 run() 据此拒绝启动），cert 路由绝不在无认证下裸露。此前该
// 不变量仅靠读代码自证；本测试将其锁死。
//
// 可在无 redis/mongo 下运行：配置校验在触碰 redis 前返回；ginRedis.NewSessionProvider
// 只包装 client、不拨号（已核实其实现）。

import (
	"testing"

	goRedis "github.com/redis/go-redis/v9"
	"github.com/spf13/viper"

	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
)

// 非拨号 redis 客户端：NewClient 构造时不连接，供装配校验用。
func nonDialingRedis() *goRedis.Client {
	return goRedis.NewClient(&goRedis.Options{Addr: "127.0.0.1:0"})
}

func TestBuildSessionProvider_FailClosed(t *testing.T) {
	cases := []struct {
		name    string
		set     map[string]any
		wantErr bool
	}{
		{
			name:    "完全缺失会话配置即拒启",
			set:     map[string]any{},
			wantErr: true,
		},
		{
			name: "缺 session_encrypted_key 即拒启",
			set: map[string]any{
				"session.cookie.name":   "ecmdb-token-key",
				"session.cookie.domain": ".example.com",
			},
			wantErr: true,
		},
		{
			name: "缺 cookie.name 即拒启",
			set: map[string]any{
				"session.session_encrypted_key": "k",
				"session.cookie.domain":         ".example.com",
			},
			wantErr: true,
		},
		{
			name: "缺 cookie.domain 即拒启",
			set: map[string]any{
				"session.session_encrypted_key": "k",
				"session.cookie.name":           "ecmdb-token-key",
			},
			wantErr: true,
		},
		{
			name: "三项齐全则装配成功",
			set: map[string]any{
				"session.session_encrypted_key": "shared-key",
				"session.cookie.name":           "ecmdb-token-key",
				"session.cookie.domain":         ".example.com",
			},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			viper.Reset()
			defer viper.Reset()
			for k, v := range tc.set {
				viper.Set(k, v)
			}

			sp, err := buildSessionProvider(nonDialingRedis())
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望 fail-closed 返回错误，却成功装配（无认证风险）")
				}
				return
			}
			if err != nil {
				t.Fatalf("配置齐全却装配失败: %v", err)
			}
			if sp == nil {
				t.Fatal("装配成功但 provider 为 nil")
			}
		})
	}
}

// 健康检查路径必须进白名单（无需认证）且幂等——否则探活会被 401 卡死，
// 或重复追加污染白名单。
func TestEnsureHealthWhitelisted(t *testing.T) {
	const health = "/api/v1/certs/health"

	t.Run("缺失则追加", func(t *testing.T) {
		cfg := &middleware.AuthConfig{Whitelist: []string{"/swagger/*"}}
		ensureHealthWhitelisted(cfg)
		if !contains(cfg.Whitelist, health) {
			t.Fatalf("健康检查路径未被加入白名单: %v", cfg.Whitelist)
		}
	})

	t.Run("已存在则幂等", func(t *testing.T) {
		cfg := &middleware.AuthConfig{Whitelist: []string{health}}
		ensureHealthWhitelisted(cfg)
		n := 0
		for _, w := range cfg.Whitelist {
			if w == health {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("健康检查路径应恰好出现一次，实际 %d 次: %v", n, cfg.Whitelist)
		}
	})
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

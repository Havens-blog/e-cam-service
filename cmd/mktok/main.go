// Command mktok 签发本地开发用会话 JWT（替代丢失的 mktok.py）。
//
// 用途：logquerye2e 与服务间联调（如 OpsAgent RCA → e-cam 日志联邦）需要
// Bearer 会话凭证。token 形状与 ginx session 完全一致：
//
//	HS256(session.session_encrypted_key)
//	payload = {"data": {Uid, SSID, Data{tenant_id, username}, Expiration},
//	           exp, iat, jti}
//
// 租户身份在 claims 里（与登录会话同源），不存在"客户端指定租户"旁路。
// 密钥从与运行服务相同的配置文件读取，不落盘不回显；stdout 只输出 token。
//
// 用法：
//
//	go run ./cmd/mktok -f config/prod.yaml -tenant 1 -days 30
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ecodeclub/ginx/session"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/spf13/viper"
)

// ginxClaims 复刻 ginx internal/jwt.RegisteredClaims[session.Claims] 的
// JSON 形状（Data=json:"data" + 内嵌标准 registered claims）。ginx 的
// internal 包外部不可导入，故本地同构声明保证形状一致。
type ginxClaims struct {
	Data session.Claims `json:"data"`
	jwt.RegisteredClaims
}

func main() {
	var (
		confFile string
		tenant   int64
		uid      int64
		username string
		days     int
	)
	flag.StringVar(&confFile, "f", "config/prod.yaml", "service config file (session_encrypted_key source)")
	flag.Int64Var(&tenant, "tenant", 1, "tenant id to bind into claims")
	flag.Int64Var(&uid, "uid", 0, "service uid (0 = service identity, not a real user)")
	flag.StringVar(&username, "username", "svc-opsagent", "service username in claims")
	flag.IntVar(&days, "days", 30, "token validity in days")
	flag.Parse()

	viper.SetConfigFile(confFile)
	if err := viper.ReadInConfig(); err != nil {
		fatal("read config: %v", err)
	}
	key := viper.GetString("session.session_encrypted_key")
	if key == "" {
		fatal("session.session_encrypted_key is empty in %s", confFile)
	}

	now := time.Now()
	ttl := time.Duration(days) * 24 * time.Hour
	claims := ginxClaims{
		Data: session.Claims{
			Uid:  uid,
			SSID: uuid.NewString(),
			Data: map[string]string{
				"tenant_id": fmt.Sprintf("%d", tenant),
				"username":  username,
			},
			Expiration: now.Add(ttl).UnixMilli(),
		},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(key))
	if err != nil {
		fatal("sign token: %v", err)
	}

	// 自检：本地验一次签名与 claims 形状，坏 token 不出门。
	if err := verifySelf(token, key); err != nil {
		fatal("self-verify failed: %v", err)
	}
	// stdout 只输出 token（密钥/配置不回显）。
	fmt.Println(token)
}

func verifySelf(token, key string) error {
	t, err := jwt.ParseWithClaims(token, &ginxClaims{}, func(*jwt.Token) (any, error) {
		return []byte(key), nil
	})
	if err != nil {
		return err
	}
	clm, ok := t.Claims.(*ginxClaims)
	if !ok || !t.Valid || clm.Data.Data["tenant_id"] == "" {
		return fmt.Errorf("invalid token claims")
	}
	b, _ := json.Marshal(map[string]any{
		"tenant":    clm.Data.Data["tenant_id"],
		"uid":       clm.Data.Uid,
		"expire_at": clm.ExpiresAt.Time.Format(time.RFC3339),
	})
	fmt.Fprintf(os.Stderr, "[mktok] %s\n", b)
	return nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[mktok] error: "+format+"\n", args...)
	os.Exit(1)
}

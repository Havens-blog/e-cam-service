// Package modelalias 多云同步写入实例的 model_uid 归一器。
//
// e-cam 的多云同步 executor（internal/cam/task/executor/sync_*.go）用
// `{provider}_{资源类型}` 拼接 model_uid（如 volcengine_ecs），与 CMDB 模型表
// 命名空间不一致，实例落库即成孤儿。本归一器从 e-cmdb-service（配置事实源）
// 拉取「别名 → CMDB 模型」映射缓存（/api/v1/cmdb/internal/model-aliases，
// 服务间可信调用），实例写入前将别名替换为真实模型 uid。
//
// 缓存策略：启动即拉取，之后每 5 分钟刷新；刷新失败保留旧缓存（fail-open，
// 未命中映射的 uid 原样返回，不阻断写入）。
package modelalias

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/auth"
	"github.com/gotomicro/ego/core/elog"
)

// Resolver 别名归一接口（实例服务注入用）。
type Resolver interface {
	// Resolve 命中映射返回目标模型 uid，否则原样返回。
	Resolve(modelUID string) string
}

// RemoteResolver 基于 e-cmdb-service 内部 API 的别名归一器。
type RemoteResolver struct {
	baseURL       string
	tokenProvider auth.TokenProvider
	client        *http.Client
	logger        *elog.Component

	mu    sync.RWMutex
	cache map[string]string
}

// NewRemoteResolver 创建归一器（baseURL 为 e-cmdb-service 内网地址）。
func NewRemoteResolver(baseURL string, tokenProvider auth.TokenProvider, logger *elog.Component) *RemoteResolver {
	if logger == nil {
		logger = elog.DefaultLogger
	}
	return &RemoteResolver{
		baseURL:       baseURL,
		tokenProvider: tokenProvider,
		client:        &http.Client{Timeout: 5 * time.Second},
		logger:        logger,
		cache:         map[string]string{},
	}
}

// Start 启动后台刷新（立即一次 + 每 5 分钟）。
func (r *RemoteResolver) Start(ctx context.Context) {
	if r.baseURL == "" {
		r.logger.Warn("model alias resolver disabled: empty cmdb base url")
		return
	}
	if err := r.refresh(ctx); err != nil {
		r.logger.Warn("model alias resolver initial refresh failed", elog.FieldErr(err))
	}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := r.refresh(ctx); err != nil {
					r.logger.Warn("model alias resolver refresh failed", elog.FieldErr(err))
				}
			}
		}
	}()
}

// refresh 拉取全量映射并替换缓存。
func (r *RemoteResolver) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/api/v1/cmdb/internal/model-aliases", nil)
	if err != nil {
		return fmt.Errorf("build alias request: %w", err)
	}
	if r.tokenProvider != nil {
		tok, err := r.tokenProvider.TokenFor(ctx, 0)
		if err != nil {
			return fmt.Errorf("获取服务令牌失败: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch model aliases: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		Code int               `json:"code"`
		Msg  string            `json:"msg"`
		Data map[string]string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("decode model aliases: %w", err)
	}
	if body.Code != 0 {
		return fmt.Errorf("model aliases upstream code=%d msg=%s", body.Code, body.Msg)
	}

	r.mu.Lock()
	r.cache = body.Data
	r.mu.Unlock()
	return nil
}

// Resolve 命中映射返回目标模型 uid，否则原样返回。
func (r *RemoteResolver) Resolve(modelUID string) string {
	if modelUID == "" {
		return modelUID
	}
	r.mu.RLock()
	target, ok := r.cache[modelUID]
	r.mu.RUnlock()
	if ok && target != "" {
		return target
	}
	return modelUID
}

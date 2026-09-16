// Search/Aggregate 结果缓存(SWR,proposal «Selected»"后台预热 + 缓存命中
// 日志"与任务实现注记 3"返回已缓存过期的旧清单+后台刷新(标注 fresh 与否)"):
//   - 新鲜窗(60s)内同参重复请求直接返回(热查询 <300ms 目标);
//   - 宽限窗(10min)内供旧结果并后台刷新(不阻塞请求,响应标注 cache_stale);
//   - 后台刷新失败退避 30s 续供旧值,防故障期反复打引擎;
//   - 完全过期/缺失同步计算(与无缓存行为一致)。
//
// 缓存不放大 SLS 扫描量:命中/供旧均少打一次引擎,后台刷新至多补回一次,
// 总量不多于无缓存的逐请求查询。
package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/gotomicro/ego/core/elog"
)

const (
	// resultFreshTTL 新鲜窗:窗口内同参请求直接命中。
	resultFreshTTL = 60 * time.Second
	// resultStaleGrace 宽限窗:新鲜期后仍供旧结果(期间后台刷新)。
	resultStaleGrace = 10 * time.Minute
	// resultRefreshBackoff 后台刷新失败后的重试退避(退避期续供旧值)。
	resultRefreshBackoff = 30 * time.Second
	// maxResultCacheEntries 键数上限(防长尾查询无限增长)。
	maxResultCacheEntries = 512
)

type resultCacheEntry struct {
	resp       any
	freshUntil time.Time
	expires    time.Time // 超过则同步重取(宽限上限)
}

// resultCache 进程内 SWR 结果缓存(键含端点+租户+全部请求参数)。
type resultCache struct {
	mu       sync.Mutex
	entries  map[string]resultCacheEntry
	inflight map[string]bool // 后台刷新单飞
}

func newResultCache() *resultCache {
	return &resultCache{
		entries:  make(map[string]resultCacheEntry),
		inflight: make(map[string]bool),
	}
}

// get 命中返回缓存结果;宽限内供旧+后台刷新;完全过期/缺失同步 compute。
// cached=结果来自缓存;stale=宽限旧值(后台刷新中)。
func (c *resultCache) get(ctx context.Context, key string, compute func(context.Context) (any, error)) (resp any, cached, stale bool, err error) {
	c.mu.Lock()
	e, ok := c.entries[key]
	c.mu.Unlock()
	if ok {
		now := time.Now()
		if now.Before(e.freshUntil) {
			return e.resp, true, false, nil
		}
		if now.Before(e.expires) {
			c.refreshAsync(key, compute)
			return e.resp, true, true, nil
		}
	}
	resp, err = compute(ctx)
	if err != nil {
		return nil, false, false, err
	}
	c.store(key, resp)
	return resp, false, false, nil
}

// refreshAsync 后台刷新(单飞;成功换新,失败退避续供旧值)。
func (c *resultCache) refreshAsync(key string, compute func(context.Context) (any, error)) {
	c.mu.Lock()
	if c.inflight[key] {
		c.mu.Unlock()
		return
	}
	c.inflight[key] = true
	c.mu.Unlock()
	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.inflight, key)
			c.mu.Unlock()
		}()
		// 脱离请求 ctx:请求返回后刷新仍要跑完
		ctx, cancel := context.WithTimeout(context.Background(), FederationTimeout)
		defer cancel()
		resp, err := compute(ctx)
		if err != nil {
			elog.Warn("[logquery] result cache background refresh failed",
				elog.String("key_prefix", keyPrefix(key)), elog.FieldErr(err))
			c.mu.Lock()
			if e, ok := c.entries[key]; ok {
				e.freshUntil = time.Now().Add(resultRefreshBackoff)
				c.entries[key] = e
			}
			c.mu.Unlock()
			return
		}
		c.store(key, resp)
	}()
}

// store 写入(超限先清过期,仍超限整体重置:进程级缓存,重建后自然回填)。
func (c *resultCache) store(key string, resp any) {
	now := time.Now()
	c.mu.Lock()
	if len(c.entries) >= maxResultCacheEntries {
		for k, e := range c.entries {
			if now.After(e.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= maxResultCacheEntries {
			c.entries = make(map[string]resultCacheEntry)
		}
	}
	c.entries[key] = resultCacheEntry{
		resp:       resp,
		freshUntil: now.Add(resultFreshTTL),
		expires:    now.Add(resultStaleGrace),
	}
	c.mu.Unlock()
}

// forceStale 测试用:条目转入宽限期(保数据,新鲜窗归零)。
func (c *resultCache) forceStale(key string) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		e.freshUntil = time.Now().Add(-time.Second)
		c.entries[key] = e
	}
	c.mu.Unlock()
}

// cachedCall Search/Aggregate 共用的缓存包装:同参命中直返(宽限期内供旧并
// 后台刷新),缺失同步 compute;返回缓存本体的浅拷贝(缓存本体只读,调用方
// 可安全写入按次标注 Cached/CacheStale,不影响并发读者)。
func cachedCall[T any](ctx context.Context, c *resultCache, kind string, tenantID int64, req any, compute func(context.Context) (*T, error)) (*T, bool, bool, error) {
	v, cached, stale, err := c.get(ctx, cacheKey(kind, tenantID, req), func(cctx context.Context) (any, error) {
		return compute(cctx)
	})
	if err != nil {
		return nil, false, false, err
	}
	out := *v.(*T)
	return &out, cached, stale, nil
}

// cacheKey 请求维度缓存键(端点 + 租户 + 全部请求参数;json 序列化字段序
// 确定性保证同参同键)。
func cacheKey(kind string, tenantID int64, req any) string {
	b, _ := json.Marshal(struct {
		Tenant int64 `json:"tenant"`
		Req    any   `json:"req"`
	}{Tenant: tenantID, Req: req})
	return kind + ":" + string(b)
}

// keyPrefix 日志用键前缀(截 JSON 首段,不整串倾倒)。
func keyPrefix(key string) string {
	if i := strings.IndexByte(key, ':'); i >= 0 {
		return key[:i]
	}
	return key
}

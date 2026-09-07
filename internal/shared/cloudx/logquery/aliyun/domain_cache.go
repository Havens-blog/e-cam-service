// 域名枚举缓存:域名分布分钟级稳定,sources 页每次切换 Tab/云都会重拉,
// 缓存 10 分钟内零 API 调用(SLS SQL 分组 ~1.5s/条,混装源串行曾是
// WAF sources 5s 的主因)。
package aliyun

import (
	"sync"
	"time"
)

// domainCacheTTL 域名枚举缓存时长。
const domainCacheTTL = 10 * time.Minute

// domainCache 进程内 TTL 缓存(键 = project/logstore/kind 摘要)。
type domainCache struct {
	mu      sync.RWMutex
	entries map[string]domainCacheEntry
}

type domainCacheEntry struct {
	domains []string
	expires time.Time
}

func newDomainCache() *domainCache {
	return &domainCache{entries: make(map[string]domainCacheEntry)}
}

// get 命中返回缓存副本;过期/缺失调用 fetch 回填。
func (c *domainCache) get(key string, fetch func() []string) []string {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if ok && time.Now().Before(e.expires) {
		out := make([]string, len(e.domains))
		copy(out, e.domains)
		return out
	}
	domains := fetch()
	if len(domains) > 0 {
		c.set(key, domains)
	}
	return domains
}

// set 写入(TTL 重置;空结果不缓存——源数据未就绪时不长期锁死)。
func (c *domainCache) set(key string, domains []string) {
	if len(domains) == 0 {
		return
	}
	cp := make([]string, len(domains))
	copy(cp, domains)
	c.mu.Lock()
	c.entries[key] = domainCacheEntry{domains: cp, expires: time.Now().Add(domainCacheTTL)}
	c.mu.Unlock()
}

// expire 强制失效(测试用)。
func (c *domainCache) expire(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// domainEnumCache 进程级共享实例(provider 每账号一个,缓存跨实例共享)。
var domainEnumCache = newDomainCache()

// logstoreEnumCache logstore 清单缓存(与域名枚举同型:清单分钟级稳定,
// Search/Aggregate/ListLogSources 每请求都重复 ListLogStore 曾是 SLB
// sources 与查询延迟的固定开销)。
var logstoreEnumCache = newDomainCache()

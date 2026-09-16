// LTS 流清单缓存:流清单分钟级稳定,每次 sources 都 ListLogStreams ×2 组
// (实测 ~300ms/请求)曾是 huawei sources 热查询的固定开销;缓存 10 分钟内
// 零 API 调用(与 aliyun 域名/logstore、aws 前缀发现同型的进程级缓存)。
package huawei

import (
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
)

// sourceCacheTTL 流清单缓存时长。
const sourceCacheTTL = 10 * time.Minute

type sourceCacheEntry struct {
	sources []logquery.LogSource
	expires time.Time
}

// sourceCache 进程内 TTL 缓存(键 = 账号/日志类型)。
type sourceCache struct {
	mu      sync.RWMutex
	entries map[string]sourceCacheEntry
}

var sourceEnumCache = &sourceCache{entries: make(map[string]sourceCacheEntry)}

// get 命中返回缓存副本(hit=true);过期/缺失调用 fetch 回填(成功且非空
// 才缓存;失败/空结果不缓存,不长期锁死)。
func (c *sourceCache) get(key string, fetch func() ([]logquery.LogSource, error)) ([]logquery.LogSource, bool, error) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if ok && time.Now().Before(e.expires) {
		return copySources(e.sources), true, nil
	}
	sources, err := fetch()
	if err != nil || len(sources) == 0 {
		return sources, false, err
	}
	c.mu.Lock()
	c.entries[key] = sourceCacheEntry{sources: copySources(sources), expires: time.Now().Add(sourceCacheTTL)}
	c.mu.Unlock()
	return sources, false, nil
}

// copySources 浅拷贝切片(LogSource 全值字段,共享底层数组安全)。
func copySources(in []logquery.LogSource) []logquery.LogSource {
	out := make([]logquery.LogSource, len(in))
	copy(out, in)
	return out
}

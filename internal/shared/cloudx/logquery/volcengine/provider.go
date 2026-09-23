// Package volcengine 火山引擎日志查询 provider。
//
// 火山云 CDN(加速)/WAF/CLB 访问日志均投递到 TLS(火山日志服务),与阿里 SLS /
// 腾讯 CLS 同构:源枚举 = DescribeTopics + 采样分类,Search = SearchLogs 分页
// 拉取映射统一模型,聚合 = SearchLogs `| select` 分析 SQL。
package volcengine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"github.com/volcengine/volc-sdk-golang/service/tls"
)

// endpointFor 按 region 拼 TLS endpoint。
func endpointFor(region string) string {
	return "https://tls-" + region + ".volces.com"
}

// defaultEnumRegions 源枚举兜底区域(账号 Regions 交集优先)。
var defaultEnumRegions = []string{"cn-guangzhou", "cn-beijing", "cn-shanghai"}

const topicKindTTL = 30 * time.Minute

// topicKindCache 进程级 topic 分类缓存(region/topic → cdn|waf|slb|other)。
type topicKindCache struct {
	mu      sync.Mutex
	entries map[string]topicKindEntry
}

type topicKindEntry struct {
	kind    string
	expires time.Time
}

var topicKindCacheInst = &topicKindCache{entries: make(map[string]topicKindEntry)}

func (c *topicKindCache) get(region, topicID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[region+"/"+topicID]
	if !ok || time.Now().After(e.expires) {
		return "", false
	}
	return e.kind, true
}

func (c *topicKindCache) set(region, topicID, kind string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[region+"/"+topicID] = topicKindEntry{kind: kind, expires: time.Now().Add(topicKindTTL)}
}

// topicsCache 进程级 DescribeTopics 清单缓存(每区域)。
type topicsCache struct {
	mu      sync.Mutex
	entries map[string]topicsEntry
}

type topicsEntry struct {
	topics  []*tls.Topic
	expires time.Time
}

var topicsCacheInst = &topicsCache{entries: make(map[string]topicsEntry)}

// provider 火山云 TLS 日志 provider。
type provider struct {
	logType logquery.LogType
	account *domain.CloudAccount
	logger  *elog.Component
}

func init() {
	// 仅 SLB(负载均衡访问日志):火山 CDN/WAF 托管产品日志未投递至 TLS(控台
	// 实时日志未开),本 provider 把火山自采的 nginx/gateway/lb 访问日志映射到
	// SLB 类型。CDN/WAF 产品日志开启投递后按独立源补。
	logquery.RegisterProvider(domain.CloudProviderVolcengine, logquery.LogTypeSLB, newProvider(logquery.LogTypeSLB))
}

func newProvider(logType logquery.LogType) logquery.ProviderCreator {
	return func(account *domain.CloudAccount) (logquery.LogProvider, error) {
		if account.AccessKeyID == "" || account.AccessKeySecret == "" {
			return nil, fmt.Errorf("volcengine logquery: account %d missing credentials", account.ID)
		}
		return &provider{logType: logType, account: account, logger: elog.DefaultLogger}, nil
	}
}

// Cloud 实现 LogProvider。
func (p *provider) Cloud() domain.CloudProvider { return domain.CloudProviderVolcengine }

// LogType 实现 LogProvider。
func (p *provider) LogType() logquery.LogType { return p.logType }

// enumRegions 枚举区域:账号 Regions 与默认落点交集(有则用),兜底默认。
func (p *provider) enumRegions() []string {
	if len(p.account.Regions) > 0 {
		have := make(map[string]bool, len(p.account.Regions))
		for _, r := range p.account.Regions {
			have[r] = true
		}
		var out []string
		for _, r := range defaultEnumRegions {
			if have[r] {
				out = append(out, r)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return defaultEnumRegions
}

// tlsClient 惰性建 TLS client(每 region;SDK 的 client 无状态仅持有 AK/SK)。
func (p *provider) tlsClient(region string) tls.Client {
	return tls.NewClient(endpointFor(region), p.account.AccessKeyID, p.account.AccessKeySecret, "", region)
}

// listTopicsCached DescribeTopics 清单(带缓存,每区域;topic 清单分钟级稳定)。
func (p *provider) listTopicsCached(ctx context.Context, region string) []*tls.Topic {
	topicsCacheInst.mu.Lock()
	if e, ok := topicsCacheInst.entries[region]; ok && time.Now().Before(e.expires) {
		topicsCacheInst.mu.Unlock()
		return e.topics
	}
	topicsCacheInst.mu.Unlock()

	client := p.tlsClient(region)
	var all []*tls.Topic
	pageNumber := 1
	const pageSize = 100
	for {
		resp, err := client.DescribeTopics(&tls.DescribeTopicsRequest{
			ProjectID: "",
			PageNumber: pageNumber,
			PageSize:   pageSize,
		})
		if err != nil {
			p.logger.Warn("[logquery-volcengine] describe topics failed",
				elog.String("region", region), elog.FieldErr(err))
			return all
		}
		if resp == nil || len(resp.Topics) == 0 {
			return all
		}
		all = append(all, resp.Topics...)
		if len(resp.Topics) < pageSize {
			break
		}
		pageNumber++
	}
	if len(all) > 0 {
		topicsCacheInst.mu.Lock()
		topicsCacheInst.entries[region] = topicsEntry{topics: all, expires: time.Now().Add(topicKindTTL)}
		topicsCacheInst.mu.Unlock()
	}
	return all
}

// probeRawLog 采样 topic 最近一条日志(SearchLogs limit 1)。失败返回 nil。
// 采样也充当首条数据 → 供 topicKind 分型与字段识别。
func (p *provider) probeRawLog(ctx context.Context, region, topicID string) map[string]any {
	client := p.tlsClient(region)
	now := time.Now().UnixMilli()
	resp, err := client.SearchLogs(&tls.SearchLogsRequest{
		TopicID:   topicID,
		Query:     "*",
		StartTime: now - 7*24*3600_000,
		EndTime:   now,
		Limit:     1,
		Sort:      "desc",
	})
	if err != nil {
		p.logger.Warn("[logquery-volcengine] probe topic failed",
			elog.String("region", region), elog.String("topic", topicID), elog.FieldErr(err))
		return nil
	}
	if resp == nil || len(resp.Logs) == 0 {
		return nil
	}
	return resp.Logs[0]
}

// topicKind 判定 topic 类型:名字非访问日志形态直接 other(不采样);访问形态
// 才采样一条按 schema 分类。0 条/无特征 → "other" 且不缓存(投递可能晚来);
// 有特征 → 缓存 30min。名字预过滤把 655 topic 的采样收敛到 ~250。
func (p *provider) topicKind(ctx context.Context, region, topicID, topicName string) string {
	if k, ok := topicKindCacheInst.get(region, topicID); ok {
		return k
	}
	if !accessNamePlausible(topicName) {
		return "other"
	}
	raw := p.probeRawLog(ctx, region, topicID)
	kind := "other"
	if len(raw) > 0 {
		if isEdgeAccessLog(raw, topicName) {
			kind = "slb"
		}
	}
	if len(raw) > 0 {
		topicKindCacheInst.set(region, topicID, kind)
	}
	return kind
}

// accessNamePlausible 名字是否为访问日志形态(含 "access" 且非 tomcat-access)。
func accessNamePlausible(topicName string) bool {
	name := strings.ToLower(topicName)
	return strings.Contains(name, "access") && !strings.Contains(name, "tomcat-access")
}

// isEdgeAccessLog 判定 topic 是否为边缘(nginx/gateway/LB)访问日志:
//   - 访问日志 schema:host + status 齐全;
//   - ALB 访问日志(火山 ALB `*_lb_access`):有 loadbalancer_id/listener_id
//     标记,**没有 method 字段**(请求整行在 request 字段);
//   - nginx/gateway 访问日志:有 method 或 request;
//   - 名字含 "access"(排除 tomcat-access)。
func isEdgeAccessLog(raw map[string]any, topicName string) bool {
	if !accessNamePlausible(topicName) {
		return false
	}
	if raw["http_host"] == nil && raw["host"] == nil {
		return false
	}
	if raw["status"] == nil {
		return false
	}
	if raw["loadbalancer_id"] != nil || raw["listener_id"] != nil {
		return true // 火山 ALB 访问日志
	}
	return raw["method"] != nil || raw["request"] != nil // nginx/gateway
}

// kindForLogType 日志类型 → topic 分类(kind)。当前只注册 SLB,固定 "slb"。
func (p *provider) kindForLogType() string { return "slb" }

// ListLogSources 枚举日志源:DescribeTopics 全部 topic → 并发采样分型 → 按 kind 归类。
func (p *provider) ListLogSources(ctx context.Context, account *domain.CloudAccount) ([]logquery.LogSource, error) {
	kind := p.kindForLogType()
	label := map[string]string{"cdn": "CDN", "waf": "WAF", "slb": "CLB"}[kind]
	refs := p.classifiedTopics(ctx, kind, nil)
	out := make([]logquery.LogSource, 0, len(refs))
	for _, ref := range refs {
		name := ref.topicName
		if name == "" {
			name = ref.topicID
		}
		out = append(out, logquery.LogSource{
			Cloud:       domain.CloudProviderVolcengine,
			AccountID:   fmt.Sprintf("%d", p.account.ID),
			AccountName: p.account.Name,
			Region:      ref.region,
			LogType:     p.logType,
			ResourceID:  ref.topicID,
			Name:        label + " / " + name,
			Enabled:     true,
			Note:        "火山 TLS 自采访问日志(实例 nginx/gateway/LB)",
		})
	}
	return out, nil
}

// Search 查询窗口内日志:逐 topic SearchLogs 分页拉取映射统一模型。
func (p *provider) Search(ctx context.Context, account *domain.CloudAccount, params logquery.SearchParams) ([]logquery.LogEntry, error) {
	if params.EndTime <= params.StartTime {
		return nil, fmt.Errorf("volcengine logquery: invalid time window")
	}
	limit := params.Limit
	if limit <= 0 || limit > 3000 {
		limit = 100
	}
	want := make(map[string]bool, len(params.Resources))
	for _, r := range params.Resources {
		if r != "" {
			want[r] = true
		}
	}
	targets := p.classifiedTopics(ctx, p.kindForLogType(), want)
	query := "*"
	if q := strings.TrimSpace(params.Query); q != "" && q != "*" {
		query = q
	}

	results := make([][]logquery.LogEntry, len(targets))
	var wg sync.WaitGroup
	for i, tgt := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			meta := logquery.LogMeta{
				Cloud:       domain.CloudProviderVolcengine,
				AccountID:   fmt.Sprintf("%d", p.account.ID),
				AccountName: p.account.Name,
				Region:      tgt.region,
				ResourceID:  tgt.topicID,
				Source:      tgt.region + "/" + tgt.topicID,
			}
			entries, err := p.searchLogs(ctx, tgt.region, tgt.topicID, params.StartTime, params.EndTime, query, limit, meta)
			if err != nil {
				p.logger.Warn("[logquery-volcengine] search topic failed",
					elog.String("region", tgt.region), elog.String("topic", tgt.topicID), elog.FieldErr(err))
				return
			}
			var filtered []logquery.LogEntry
			for _, e := range entries {
				if logquery.EntryMatches(e, params.Filters) {
					filtered = append(filtered, e)
				}
			}
			results[i] = filtered
		}()
	}
	wg.Wait()

	var entries []logquery.LogEntry
	for _, r := range results {
		entries = append(entries, r...)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].GetTimestamp() > entries[j].GetTimestamp()
	})
	const providerCap = 3000
	if len(entries) > providerCap {
		entries = entries[:providerCap]
	}
	return entries, nil
}

// topicsByKind 解析目标 topic 列表(kind 固定 "slb");Resources 收敛(topicId)。
// topicRef 定位一个 TLS topic。
type topicRef struct {
	region    string
	topicID   string
	topicName string
}

// classifiedTopics 并发分型出指定 kind 的 topic。名字非访问形态不采样(topicKind
// 内预过滤);resources 收敛先于采样,避免无谓 SearchLogs。并发上限 16。
func (p *provider) classifiedTopics(ctx context.Context, kind string, want map[string]bool) []topicRef {
	var all []topicRef
	for _, region := range p.enumRegions() {
		for _, t := range p.listTopicsCached(ctx, region) {
			if t == nil || t.TopicID == "" {
				continue
			}
			if len(want) > 0 && !want[t.TopicID] {
				continue
			}
			all = append(all, topicRef{region: region, topicID: t.TopicID, topicName: t.TopicName})
		}
	}
	sem := make(chan struct{}, 16)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var out []topicRef
	for _, ref := range all {
		wg.Add(1)
		go func(ref topicRef) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if p.topicKind(ctx, ref.region, ref.topicID, ref.topicName) == kind {
				mu.Lock()
				out = append(out, ref)
				mu.Unlock()
			}
		}(ref)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].topicName < out[j].topicName })
	return out
}

// searchLogs 单 topic SearchLogs 分页拉取映射(降序,凑满 limit 即停)。
func (p *provider) searchLogs(ctx context.Context, region, topicID string, startMs, endMs int64, query string, limit int, meta logquery.LogMeta) ([]logquery.LogEntry, error) {
	client := p.tlsClient(region)
	var out []logquery.LogEntry
	offset := int64(0)
	const pageSize = 100
	for int64(len(out)) < int64(limit) {
		resp, err := client.SearchLogs(&tls.SearchLogsRequest{
			TopicID:   topicID,
			Query:     query,
			StartTime: startMs,
			EndTime:   endMs,
			Limit:     pageSize,
			Sort:      "desc",
			Offset:    &offset,
		})
		if err != nil {
			return nil, fmt.Errorf("tls searchlogs %s: %w", topicID, err)
		}
		if resp == nil || len(resp.Logs) == 0 {
			break
		}
		for _, raw := range resp.Logs {
			out = append(out, mapAccessLog(meta, raw))
		}
		if len(resp.Logs) < pageSize || resp.ListOver {
			break
		}
		offset += int64(len(resp.Logs))
	}
	return out, nil
}
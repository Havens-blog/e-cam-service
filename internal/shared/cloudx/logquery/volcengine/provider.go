// Package volcengine 火山引擎日志查询 provider。
//
// 火山云日志均投递到 TLS(火山日志服务):负载均衡(ALB `*_lb_access`,原生 schema
// loadbalancer_id/listener_id)与自采 nginx/gateway 访问日志(`*_access`),映射到
// SLB 类型。CDN 加速实时日志 / WAF 日志未投递(控台实时日志未开),暂不纳入。
// 与阿里 SLS / 腾讯 CLS 同构:源枚举 = DescribeTopics + 名字分型(无采样),
// Search = SearchLogs 分页拉取映射统一模型。
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

// topicsTTL DescribeTopics 清单缓存新鲜时长(topic 清单分钟级稳定)。
const topicsTTL = 30 * time.Minute

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
	// 仅 SLB(负载均衡访问日志):火山 ALB 访问日志(`*_lb_access`)与自采 nginx/
	// gateway 访问日志(`*_access`)映射到 SLB。CDN/WAF 托管产品日志未投递 TLS。
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

// listTopicsCached DescribeTopics 清单(带缓存,每区域)。
func (p *provider) listTopicsCached(ctx context.Context, region string) []*tls.Topic {
	topicsCacheInst.mu.Lock()
	if e, ok := topicsCacheInst.entries[region]; ok && time.Now().Before(e.expires) {
		topicsCacheInst.mu.Unlock()
		return e.topics
	}
	topicsCacheInst.mu.Unlock()

	client := p.tlsClient(region)
	const pageSize = 100
	// DescribeTopics 每页固定延迟大(~4s),1130 topic 串行 12 页 ~50s 冷枚举。
	// 首页拿 Total 后并发拉余页,冷枚举收敛到 ~单页 + 一批并发页。
	first, err := client.DescribeTopics(&tls.DescribeTopicsRequest{
		ProjectID: "",
		PageNumber: 1,
		PageSize:   pageSize,
	})
	if err != nil {
		p.logger.Warn("[logquery-volcengine] describe topics failed",
			elog.String("region", region), elog.FieldErr(err))
		return nil
	}
	if first == nil || len(first.Topics) == 0 {
		return nil
	}
	all := first.Topics
	pages := (first.Total + pageSize - 1) / pageSize
	if pages > 1 {
		slots := make([][]*tls.Topic, pages+1)
		slots[1] = first.Topics
		var wg sync.WaitGroup
		sem := make(chan struct{}, 12)
		for pn := 2; pn <= pages; pn++ {
			wg.Add(1)
			go func(pn int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				resp, err := client.DescribeTopics(&tls.DescribeTopicsRequest{
					ProjectID: "", PageNumber: pn, PageSize: pageSize,
				})
				if err != nil {
					p.logger.Warn("[logquery-volcengine] describe topics page failed",
						elog.String("region", region), elog.Int("page", pn), elog.FieldErr(err))
					return
				}
				if resp != nil {
					slots[pn] = resp.Topics
				}
			}(pn)
		}
		wg.Wait()
		for pn := 2; pn <= pages; pn++ {
			all = append(all, slots[pn]...)
		}
	}
	if len(all) > 0 {
		topicsCacheInst.mu.Lock()
		topicsCacheInst.entries[region] = topicsEntry{topics: all, expires: time.Now().Add(topicsTTL)}
		topicsCacheInst.mu.Unlock()
	}
	return all
}

// isAccessTopic 名字是否为访问日志形态:含 "access"(覆盖 *_access /
// *-nginx-access / *-gateway-access / *_lb_access / *-cache-access),排除
// *-tomcat-access(应用服务器访问,非边缘)。-business/-error 不含 "access" 自然
// 排除。采集管道命名稳定,纯名字分型免去逐 topic 采样(采样曾是冷枚举 ~73s 主因)。
func isAccessTopic(topicName string) bool {
	name := strings.ToLower(topicName)
	return strings.Contains(name, "access") && !strings.Contains(name, "tomcat-access")
}

// topicRef 定位一个 TLS 访问日志 topic。
type topicRef struct {
	region    string
	topicID   string
	topicName string
}

// accessTopics 列出全部访问日志 topic(纯名字过滤,不采样)。want 非空时按
// 名字或 ID 收敛(源清单 ResourceID=名字,历史裸 ID 亦兼容)。
func (p *provider) accessTopics(ctx context.Context, want map[string]bool) []topicRef {
	var out []topicRef
	for _, region := range p.enumRegions() {
		for _, t := range p.listTopicsCached(ctx, region) {
			if t == nil || t.TopicID == "" || !isAccessTopic(t.TopicName) {
				continue
			}
			if len(want) > 0 && !want[t.TopicName] && !want[t.TopicID] {
				continue
			}
			out = append(out, topicRef{region: region, topicID: t.TopicID, topicName: t.TopicName})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].topicName < out[j].topicName })
	return out
}

// ListLogSources 枚举日志源:纯名字分型 + 按名去重(同名多 topic = 同一逻辑源
// 分片/重建)。ResourceID=名字,搜索按名收敛到全部同道 topic。
func (p *provider) ListLogSources(ctx context.Context, account *domain.CloudAccount) ([]logquery.LogSource, error) {
	refs := p.accessTopics(ctx, nil)
	seen := make(map[string]bool, len(refs))
	out := make([]logquery.LogSource, 0, len(refs))
	for _, ref := range refs {
		name := ref.topicName
		if name == "" {
			name = ref.topicID
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, logquery.LogSource{
			Cloud:       domain.CloudProviderVolcengine,
			AccountID:   fmt.Sprintf("%d", p.account.ID),
			AccountName: p.account.Name,
			Region:      ref.region,
			LogType:     p.logType,
			ResourceID:  name,
			Name:        "CLB / " + name,
			Enabled:     true,
			Note:        "火山 TLS 访问日志(ALB/nginx/gateway)",
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
	targets := p.accessTopics(ctx, want)
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
				ResourceID:  tgt.topicName,
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
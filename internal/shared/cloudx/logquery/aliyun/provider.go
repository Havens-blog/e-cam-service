// Package aliyun 阿里云日志查询 provider(Phase 1)。
//
// Phase 0 实测(source-status.md):阿里侧全部日志已汇聚 SLS,无需碰
// Kafka/日志文件包;每账号 8 类源按固定 catalog 查询:
//   - SLB:cn-shenzhen/jlc-lb-log(ALB 实例流 + 聚合流)、eu-central-1/jlc-prod-overseas-log(海外)
//   - WAF:eu-central-1 wafnew-project(WAF3.0)、cn-shenzhen 云安全中心渠道 wafng-logstore
//   - CDN:cn-shenzhen dcdn-edge-rtlog-*(DCDN 边缘实时)、jlc-prod-cdn-log(域名转存,
//     Phase 0 观察期无数据但 schema 与 DCDN 转存同构)、jlc-prod-akamai-cdnwaf-log(Akamai CDN/WAF 自采)
package aliyun

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/gotomicro/ego/core/elog"
)

func init() {
	logquery.RegisterProvider(domain.CloudProviderAliyun, logquery.LogTypeSLB, newProvider(logquery.LogTypeSLB))
	logquery.RegisterProvider(domain.CloudProviderAliyun, logquery.LogTypeWAF, newProvider(logquery.LogTypeWAF))
	logquery.RegisterProvider(domain.CloudProviderAliyun, logquery.LogTypeCDN, newProvider(logquery.LogTypeCDN))
}

// slsSource SLS 源 catalog 条目:project(含 logstore 为动态枚举)。
type slsSource struct {
	region   string
	project  string
	logType  logquery.LogType
	kind     mapperKind
	logstore string               // 空=ListLogStores 动态枚举(过滤 -metrics 等内部流)
	name     string               // 展示名(logstore 动态枚举时的前缀描述)
	fixed    []logquery.LogSource // 固定源(不枚举,直接给)
	note     string
}

// catalog 阿里侧 SLS 日志源清单(Phase 0 实测;project 名账号内稳定)。
var catalog = []slsSource{
	// ---- SLB(ALB 访问日志,topic=alb_layer7_access_log) ----
	{region: "cn-shenzhen", project: "jlc-lb-log", logType: logquery.LogTypeSLB, kind: kindALB,
		logstore: "", name: "ALB", note: "国内 ALB/CLB 访问日志(实例流+聚合)"},
	{region: "eu-central-1", project: "jlc-prod-overseas-log", logType: logquery.LogTypeSLB, kind: kindALB,
		logstore: "", name: "海外 ALB", note: "海外 ALB 访问日志"},
	// ---- WAF ----
	{region: "eu-central-1", project: "wafnew-project-1210557380197478-eu-central-1", logType: logquery.LogTypeWAF, kind: kindWAF3,
		logstore: "wafnew-logstore", name: "WAF3.0(海外)", note: "eu-central-1 WAF 日志库"},
	{region: "cn-shenzhen", project: "aliyun-cloudsiem-channel-1210557380197478-cn-shenzhen", logType: logquery.LogTypeWAF, kind: kindWAF3,
		logstore: "wafng-logstore", name: "WAF3.0(国内渠道)", note: "云安全中心渠道,与 wafnew 同 schema"},
	// ---- CDN ----
	{region: "cn-shenzhen", project: "jlc-prod-akamai-cdnwaf-log", logType: logquery.LogTypeCDN, kind: kindAkamaiCDN,
		logstore: "jlc-prod-akamai-cdn-log", name: "Akamai CDN(自采)", note: "Akamai CDN 日志自采入库"},
	{region: "cn-shenzhen", project: "dcdn-edge-rtlog-cn-42d9825f", logType: logquery.LogTypeCDN, kind: kindDCDN,
		logstore: "dcdn-edge-rtlog", name: "DCDN 边缘实时", note: "DCDN 实时日志"},
	{region: "cn-shenzhen", project: "jlc-prod-cdn-log", logType: logquery.LogTypeCDN, kind: kindDCDN,
		logstore: "", name: "CDN 域名转存", note: "离线转存,近 30 天无数据(schema 同 DCDN)"},
}

// provider 阿里云 SLS 日志 provider(单账号)。
type provider struct {
	logType   logquery.LogType
	account   *domain.CloudAccount
	logger    *elog.Component
	clientsMu sync.Mutex
	clients   map[string]*sls.Client // region -> SLS client
}

// newProvider 按日志类型构造(SLS endpoint 随 region 切换,client 惰性按 region 建)。
func newProvider(logType logquery.LogType) logquery.ProviderCreator {
	return func(account *domain.CloudAccount) (logquery.LogProvider, error) {
		if account.AccessKeyID == "" || account.AccessKeySecret == "" {
			return nil, fmt.Errorf("aliyun logquery: account %d missing credentials", account.ID)
		}
		return &provider{
			logType: logType,
			account: account,
			logger:  elog.DefaultLogger,
		}, nil
	}
}

// clients 按 region 缓存 SLS client(endpoint = <region>.log.aliyuncs.com)。
func (p *provider) clientFor(region string) *sls.Client {
	p.clientsMu.Lock()
	defer p.clientsMu.Unlock()
	if p.clients == nil {
		p.clients = make(map[string]*sls.Client)
	}
	if c, ok := p.clients[region]; ok {
		return c
	}
	c := &sls.Client{
		Endpoint:        fmt.Sprintf("%s.log.aliyuncs.com", region),
		AccessKeyID:     p.account.AccessKeyID,
		AccessKeySecret: p.account.AccessKeySecret,
		RequestTimeOut:  20 * time.Second,
		RetryTimeOut:    40 * time.Second,
	}
	p.clients[region] = c
	return c
}

// Cloud 实现 LogProvider。
func (p *provider) Cloud() domain.CloudProvider { return domain.CloudProviderAliyun }

// LogType 实现 LogProvider。
func (p *provider) LogType() logquery.LogType { return p.logType }

// ListLogSources 枚举该账号该类型日志源:SLS ListLogStores + catalog 元数据。
func (p *provider) ListLogSources(ctx context.Context, account *domain.CloudAccount) ([]logquery.LogSource, error) {
	var out []logquery.LogSource
	for _, src := range catalog {
		if src.logType != p.logType {
			continue
		}
		if src.logstore != "" {
			out = append(out, p.toSource(src, src.logstore, true))
			continue
		}
		// 动态枚举 logstore(过滤 metrics/diagnostic 内部流)
		stores, err := p.clientFor(src.region).ListLogStore(src.project)
		if err != nil {
			// 枚举失败不阻塞其他源(联邦隔离原则)
			p.logger.Warn("[logquery-aliyun] list logstores failed",
				elog.String("project", src.project), elog.FieldErr(err))
			continue
		}
		for _, ls := range stores {
			if strings.HasSuffix(ls, "-metrics") || strings.HasSuffix(ls, "-metrics-result") ||
				ls == "internal-ml-log" || strings.HasPrefix(ls, "internal-") {
				continue
			}
			out = append(out, p.toSource(src, ls, true))
		}
	}
	return out, nil
}

// toSource catalog 条目 -> LogSource。
func (p *provider) toSource(src slsSource, logstore string, enabled bool) logquery.LogSource {
	return logquery.LogSource{
		Cloud:       domain.CloudProviderAliyun,
		AccountID:   fmt.Sprintf("%d", p.account.ID),
		AccountName: p.account.Name,
		Region:      src.region,
		LogType:     src.logType,
		ResourceID:  logstore,
		Name:        src.name + " / " + logstore,
		Enabled:     enabled,
		Note:        src.note,
	}
}

// Search 查询窗口内日志并映射统一模型:逐 catalog 查询,单源失败跳过并记日志,
// 结果按时间倒序(联邦编排层再做跨源归并)。
func (p *provider) Search(ctx context.Context, account *domain.CloudAccount, params logquery.SearchParams) ([]logquery.LogEntry, error) {
	if params.EndTime <= params.StartTime {
		return nil, fmt.Errorf("aliyun logquery: invalid time window")
	}
	limit := params.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100 // SLS GetLogs 单次 Lines<=100,分页到 limit
	}
	resourceFilter := make(map[string]bool, len(params.Resources))
	for _, r := range params.Resources {
		resourceFilter[r] = true
	}
	regionFilter := make(map[string]bool, len(params.Regions))
	for _, r := range params.Regions {
		regionFilter[r] = true
	}

	// ---- 收集 fetch 目标(catalog x logstore) ----
	type fetchTarget struct {
		src      slsSource
		logstore string
	}
	var targets []fetchTarget
	for _, src := range catalog {
		if src.logType != p.logType {
			continue
		}
		if len(regionFilter) > 0 && !regionFilter[src.region] {
			continue
		}
		stores := []string{src.logstore}
		if src.logstore == "" {
			ls, err := p.clientFor(src.region).ListLogStore(src.project)
			if err != nil {
				p.logger.Warn("[logquery-aliyun] list logstores failed",
					elog.String("project", src.project), elog.FieldErr(err))
				continue
			}
			stores = filterInternalStores(ls)
		}
		for _, ls := range stores {
			if len(resourceFilter) > 0 && !resourceFilter[ls] && !resourceFilter[src.project] {
				continue
			}
			targets = append(targets, fetchTarget{src: src, logstore: ls})
		}
	}

	// ---- logstore 级并发拉取(8 并发上限,单源失败隔离) ----
	results := make([][]map[string]string, len(targets))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, tgt := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			logs, err := p.fetchLogs(ctx, tgt.src, tgt.logstore, params, limit)
			if err != nil {
				p.logger.Warn("[logquery-aliyun] fetch logs failed",
					elog.String("project", tgt.src.project), elog.String("logstore", tgt.logstore),
					elog.FieldErr(err))
				return
			}
			results[i] = logs
		}()
	}
	wg.Wait()

	// ---- 映射(顺序,无锁) ----
	var entries []logquery.LogEntry
	for i, tgt := range targets {
		if len(results[i]) == 0 {
			continue
		}
		meta := logquery.LogMeta{
			Cloud:       domain.CloudProviderAliyun,
			AccountID:   fmt.Sprintf("%d", p.account.ID),
			AccountName: p.account.Name,
			Region:      tgt.src.region,
			ResourceID:  tgt.logstore,
			Source:      tgt.src.project + "/" + tgt.logstore,
		}
		for _, raw := range results[i] {
			if e := mapEntry(tgt.src.kind, meta, raw); e != nil {
				entries = append(entries, e)
			}
		}
	}
	// 时间倒序
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].GetTimestamp() > entries[j].GetTimestamp()
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// fetchLogs 单 logstore 分页拉取(SLS 单次 Lines<=100)。
func (p *provider) fetchLogs(ctx context.Context, src slsSource, logstore string, params logquery.SearchParams, limit int) ([]map[string]string, error) {
	client := p.clientFor(src.region)
	// SLS 查询窗口为秒
	from := params.StartTime / 1000
	to := params.EndTime / 1000
	query := buildQuery(src.kind, params.Query)
	var all []map[string]string
	offset := int64(0)
	const pageSize = 100
	for int64(len(all)) < int64(limit) {
		resp, err := client.GetLogsV2(src.project, logstore, &sls.GetLogRequest{
			From:    from,
			To:      to,
			Query:   query,
			Lines:   pageSize,
			Offset:  offset,
			Reverse: true, // 时间倒序
		})
		if err != nil {
			return nil, fmt.Errorf("sls get_logs %s/%s: %w", src.project, logstore, err)
		}
		if len(resp.Logs) == 0 {
			break
		}
		all = append(all, resp.Logs...)
		if len(resp.Logs) < pageSize || !resp.IsComplete() {
			break
		}
		offset += int64(len(resp.Logs))
	}
	return all, nil
}

// buildQuery 原始查询式 + kind 专属 __topic__ 过滤(服务端过滤:
// 聚合 project 里混有 nacos/app 等非 LB 日志流,topic 是 ALB/WAF 访问日志的
// 稳定判据;无 topic 的源(DCDN rtlog/Akamai)不加过滤)。
func buildQuery(kind mapperKind, userQuery string) string {
	var topic string
	switch kind {
	case kindALB:
		topic = "__topic__:alb_layer7_access_log"
	case kindWAF3:
		topic = "__topic__:waf_access_log"
	}
	q := strings.TrimSpace(userQuery)
	if q == "" || q == "*" {
		if topic == "" {
			return "*"
		}
		return topic
	}
	if topic == "" {
		return q
	}
	return q + " and " + topic
}

// filterInternalStores 过滤 SLS 内部流(metrics/diagnostic/ml)。
func filterInternalStores(stores []string) []string {
	out := make([]string, 0, len(stores))
	for _, ls := range stores {
		if strings.HasSuffix(ls, "-metrics") || strings.HasSuffix(ls, "-metrics-result") ||
			strings.HasPrefix(ls, "internal-") {
			continue
		}
		out = append(out, ls)
	}
	return out
}
